package read

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/kaicherry/nup/format"
	"github.com/klauspost/compress/zstd"
)

type PackageInfo struct {
	Index      int
	TitleID    uint64
	Kind       uint8
	Version    uint32
	Flags      uint8
	RootDir    uint32
	ExeFSDir   uint32
	RomFSDir   uint32
	MetaOffset uint64
	MetaSize   uint32
}

func (p PackageInfo) KindName() string {
	if s, ok := format.KindNames[p.Kind]; ok {
		return s
	}
	return "kind"
}

func (p PackageInfo) VersionStr() string {
	return ""
}

type NodeRef struct {
	IsDir bool
	Index int
	Name  []byte
}

type Reader struct {
	Path        string
	f           *os.File
	data        []byte
	dataOnce    sync.Once
	fileSize    uint64
	archiveSize uint64
	blobOffset  uint64
	Super       map[string]any
	DirCount    uint64
	FileCount   uint64
	Pool        []byte
	Packages    []PackageInfo
	Dirs        [][3]uint32
	DirNames    [][]byte
	Files       [][]any
	FileNames   [][]byte
	FileInfos   [][6]uint64
	FileSHA     [][32]byte
	FileCodec   []uint8
	Children    []uint32
}

// GetData returns the full container bytes on demand.
// Large archives should use the range-based methods instead.
func (r *Reader) GetData() []byte {
	r.dataOnce.Do(func() {
		if r.fileSize > uint64(^uint(0)>>1) {
			return
		}
		data := make([]byte, int(r.fileSize))
		n, err := r.f.ReadAt(data, 0)
		if err != nil || n != len(data) {
			return
		}
		r.data = data
	})
	return r.data
}

func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if st.Size() < int64(format.HeaderSize) {
		f.Close()
		return nil, errors.New("truncated")
	}
	header := make([]byte, int(format.HeaderSize))
	if _, err := f.ReadAt(header, 0); err != nil {
		f.Close()
		return nil, err
	}
	sup, err := format.ParseSuper(header)
	if err != nil {
		f.Close()
		return nil, err
	}
	tocOff := sup["toc_offset"].(uint64)
	tocSize := sup["toc_size"].(uint64)
	blobOffset := sup["blob_offset"].(uint64)
	archiveSize := sup["file_size"].(uint64)
	fileSize := uint64(st.Size())
	if tocOff < uint64(format.HeaderSize) || tocOff > archiveSize || tocSize > archiveSize-tocOff {
		f.Close()
		return nil, errors.New("bad toc")
	}
	tocEnd := tocOff + tocSize
	if blobOffset < tocEnd {
		f.Close()
		return nil, errors.New("overlap")
	}
	if blobOffset%format.ExtentAlign != 0 {
		f.Close()
		return nil, errors.New("unaligned blob")
	}
	if archiveSize < blobOffset || archiveSize > fileSize {
		f.Close()
		return nil, errors.New("bad sizes")
	}
	if tocSize > uint64(^uint(0)>>1) {
		f.Close()
		return nil, errors.New("toc too large")
	}
	rawTOC := make([]byte, int(tocSize))
	if _, err := f.ReadAt(rawTOC, int64(tocOff)); err != nil {
		f.Close()
		return nil, err
	}
	got := sha256.Sum256(rawTOC)
	exp := sup["toc_sha256"].([32]byte)
	if got != exp {
		f.Close()
		return nil, errors.New("toc sha mismatch")
	}
	r := &Reader{
		Path:        path,
		f:           f,
		fileSize:    fileSize,
		archiveSize: archiveSize,
		blobOffset:  blobOffset,
		Super:       sup,
	}
	if err := r.parseTOC(rawTOC); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

func (r *Reader) Close() error {
	if r.f != nil {
		return r.f.Close()
	}
	return nil
}

// ReadRange loads a bounded slice from the archive.
// Large ranges should use CopyRangeTo to avoid a proportional allocation.
func (r *Reader) ReadRange(offset, size uint64) ([]byte, error) {
	if offset > r.archiveSize || size > r.archiveSize-offset {
		return nil, errors.New("oob")
	}
	if size > uint64(^uint(0)>>1) {
		return nil, errors.New("range too large")
	}
	data := make([]byte, int(size))
	n, err := r.f.ReadAt(data, int64(offset))
	if err != nil {
		return nil, err
	}
	if n != len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

// CopyRangeTo streams a bounded archive range to dst.
// It avoids allocating a buffer proportional to the range size.
func (r *Reader) CopyRangeTo(dst io.Writer, offset, size uint64) error {
	if offset > r.archiveSize || size > r.archiveSize-offset {
		return errors.New("oob")
	}
	n, err := io.CopyN(dst, io.NewSectionReader(r.f, int64(offset), int64(size)), int64(size))
	if err != nil {
		return err
	}
	if uint64(n) != size {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (r *Reader) parseTOC(toc []byte) error {
	if len(toc) < 0x30 {
		return errors.New("toc short")
	}
	if !bytes.Equal(toc[0:4], format.TOCMagic) {
		return errors.New("bad toc magic")
	}
	pkgCount := binary.LittleEndian.Uint32(toc[0x04:0x08])
	if pkgCount != r.Super["package_count"].(uint32) {
		return errors.New("pkg count mismatch")
	}
	dirCount := binary.LittleEndian.Uint64(toc[0x08:0x10])
	fileCount := binary.LittleEndian.Uint64(toc[0x10:0x18])
	poolOff := binary.LittleEndian.Uint64(toc[0x18:0x20])
	poolSize := binary.LittleEndian.Uint64(toc[0x20:0x28])
	if uint64(pkgCount) > uint64(len(toc)-0x30)/uint64(format.PkgSize) {
		return errors.New("pkg trunc")
	}
	r.DirCount = dirCount
	r.FileCount = fileCount
	pos := 0x30
	for i := uint32(0); i < pkgCount; i++ {
		p, err := format.ParsePkg(toc, pos)
		if err != nil {
			return err
		}
		if p.MetaSize > 0 && (p.MetaOffset < r.blobOffset || p.MetaOffset > r.archiveSize || uint64(p.MetaSize) > r.archiveSize-p.MetaOffset) {
			return errors.New("meta extent oob")
		}
		pos += format.PkgSize
		r.Packages = append(r.Packages, PackageInfo{
			Index:      len(r.Packages),
			TitleID:    p.TitleID,
			Kind:       p.Kind,
			Version:    p.Version,
			Flags:      p.Flags,
			RootDir:    p.RootDir,
			ExeFSDir:   p.ExeFSDir,
			RomFSDir:   p.RomFSDir,
			MetaOffset: p.MetaOffset,
			MetaSize:   p.MetaSize,
		})
	}
	tocSize := uint64(len(toc))
	if poolOff > tocSize || poolSize > tocSize-poolOff {
		return errors.New("pool oob")
	}
	poolEnd := poolOff + poolSize
	if uint64(pos) > poolOff || dirCount > (poolOff-uint64(pos))/uint64(format.DirSize) {
		return errors.New("dir trunc")
	}
	r.Pool = toc[int(poolOff):int(poolEnd)]
	// dirs
	for i := uint64(0); i < dirCount; i++ {
		if pos+format.DirSize > len(toc) {
			return errors.New("dir trunc")
		}
		off := binary.LittleEndian.Uint32(toc[pos+0x00 : pos+0x04])
		ln := binary.LittleEndian.Uint32(toc[pos+0x04 : pos+0x08])
		first := binary.LittleEndian.Uint32(toc[pos+0x08 : pos+0x0C])
		count := binary.LittleEndian.Uint32(toc[pos+0x0C : pos+0x10])
		pos += format.DirSize
		poolLen := uint64(len(r.Pool))
		if uint64(off) > poolLen || uint64(ln) > poolLen-uint64(off) {
			return errors.New("dir name oob")
		}
		r.DirNames = append(r.DirNames, append([]byte(nil), r.Pool[int(off):int(uint64(off)+uint64(ln))]...))
		r.Dirs = append(r.Dirs, [3]uint32{first, count, 0})
	}
	// files
	if uint64(pos) > poolOff || fileCount > (poolOff-uint64(pos))/uint64(format.FileSize) {
		return errors.New("file trunc")
	}
	r.FileNames = make([][]byte, 0, int(fileCount))
	r.FileSHA = make([][32]byte, 0, fileCount)
	r.FileCodec = make([]uint8, 0, fileCount)
	for i := uint64(0); i < fileCount; i++ {
		if pos+format.FileSize > len(toc) {
			return errors.New("file trunc")
		}
		off := binary.LittleEndian.Uint32(toc[pos+0x00 : pos+0x04])
		ln := binary.LittleEndian.Uint32(toc[pos+0x04 : pos+0x08])
		size := binary.LittleEndian.Uint64(toc[pos+0x08 : pos+0x10])
		eoff := binary.LittleEndian.Uint64(toc[pos+0x10 : pos+0x18])
		esz := binary.LittleEndian.Uint64(toc[pos+0x18 : pos+0x20])
		var sha [32]byte
		copy(sha[:], toc[pos+0x20:pos+0x40])
		codec := toc[pos+0x40]
		cflags := toc[pos+0x41]
		pos += format.FileSize
		if cflags != 0 {
			return errors.New("bad codec flags")
		}
		if codec != format.CodecRaw && codec != format.CodecZSTD {
			return errors.New("bad codec")
		}
		poolLen := uint64(len(r.Pool))
		if uint64(off) > poolLen || uint64(ln) > poolLen-uint64(off) {
			return errors.New("file name oob")
		}
		r.FileNames = append(r.FileNames, append([]byte(nil), r.Pool[int(off):int(uint64(off)+uint64(ln))]...))
		r.FileSHA = append(r.FileSHA, sha)
		r.FileCodec = append(r.FileCodec, codec)
		// store info
		r.FileInfos = append(r.FileInfos, [6]uint64{size, eoff, esz, uint64(codec), 0, 0})
		if size == 0 && (eoff != 0 || esz != 0 || codec != format.CodecRaw) {
			return errors.New("zero size bad")
		}
		if size != 0 && esz == 0 {
			return errors.New("bad extent")
		}
		if size != 0 && (eoff < r.blobOffset || eoff > r.archiveSize || esz > r.archiveSize-eoff) {
			return errors.New("extent oob")
		}
		if size != 0 && codec == format.CodecRaw && esz != size {
			return errors.New("raw size mismatch")
		}
	}
	// children
	slotStart := pos
	if uint64(slotStart) > poolOff || (poolOff-uint64(slotStart))%4 != 0 {
		return errors.New("children align")
	}
	cnt := int((poolOff - uint64(slotStart)) / 4)
	r.Children = make([]uint32, cnt)
	for i := 0; i < cnt; i++ {
		r.Children[i] = binary.LittleEndian.Uint32(toc[slotStart+4*i : slotStart+4*i+4])
	}
	return nil
}

func (r *Reader) FileInfo(i int) ([]byte, uint64, [32]byte, uint64, uint64, uint8) {
	fi := r.FileInfos[i]
	return r.FileNames[i], fi[0], r.FileSHA[i], fi[1], fi[2], r.FileCodec[i]
}

func (r *Reader) ReadFile(i int) ([]byte, error) {
	_, size, _, eoff, esz, codec := r.FileInfo(i)
	if size == 0 {
		return []byte{}, nil
	}
	maxInt := uint64(^uint(0) >> 1)
	if size > maxInt || esz > maxInt {
		return nil, errors.New("file too large")
	}
	if eoff < r.blobOffset || eoff > r.archiveSize || esz > r.archiveSize-eoff {
		return nil, errors.New("oob")
	}
	extent := make([]byte, int(esz))
	n, err := r.f.ReadAt(extent, int64(eoff))
	if err != nil {
		return nil, err
	}
	if n != len(extent) {
		return nil, errors.New("short")
	}
	if codec == format.CodecRaw {
		if uint64(len(extent)) != size {
			return nil, errors.New("size mismatch")
		}
		return extent, nil
	}
	if codec == format.CodecZSTD {
		d, err := zstd.NewReader(nil)
		if err != nil {
			return nil, err
		}
		defer d.Close()
		data, err := d.DecodeAll(extent, make([]byte, 0, int(size)))
		if err != nil {
			return nil, err
		}
		if uint64(len(data)) != size {
			return nil, errors.New("size mismatch")
		}
		return data, nil
	}
	return nil, errors.New("codec")
}

func (r *Reader) NodeName(ref NodeRef) []byte {
	if ref.IsDir {
		if ref.Index >= len(r.DirNames) {
			return []byte{}
		}
		return r.DirNames[ref.Index]
	}
	if ref.Index >= len(r.FileNames) {
		return []byte{}
	}
	return r.FileNames[ref.Index]
}
