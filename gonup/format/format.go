package format

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var (
	Magic    = []byte("NUP\x01")
	TOCMagic = []byte("TOC\x01")
)

const (
	HeaderSize  = 0x80
	PkgSize     = 0x40
	DirSize     = 0x18
	FileSize    = 0x50
	ExtentAlign = 16
	NODir       = 0xFFFFFFFF

	CodecRaw  = 0
	CodecZSTD = 1

	KindBase         = 0
	KindUpdate       = 1
	KindAddon        = 2
	KindAddonUpdate  = 3
	KindSystem       = 4

	TLVNPDM             = 0x0001
	TLVNACP             = 0x0002
	TLVIcon             = 0x0003
	TLVDisplayVersion   = 0x0004
	TLVTitleName        = 0x0005
	TLVPublisher        = 0x0006
	TLVBuildID          = 0x0007
	TLVSourceDump       = 0x0008
	TLVLocalizedTitles  = 0x0009
	TLVLocalizedIcons   = 0x000A

	PkgFlagStandaloneUpdate = 0x01
)

var KindNames = map[uint8]string{
	KindBase:        "base",
	KindUpdate:      "update",
	KindAddon:       "addon",
	KindAddonUpdate: "addon_update",
	KindSystem:      "system",
}

var (
	ErrBadMagic      = errors.New("bad magic")
	ErrBadVer        = errors.New("unsupported format major version")
	ErrCorrupt       = errors.New("corrupt")
)

type Package struct {
	TitleID    uint64
	RootDir    uint32
	ExeFSDir   uint32
	RomFSDir   uint32
	Version    uint32
	Kind       uint8
	Flags      uint8
	MetaOffset uint64
	MetaSize   uint32
}

type DirNode struct {
	Name       []byte
	FirstChild uint32
	ChildCount uint32
}

type FileNode struct {
	Name         []byte
	Size         uint64
	SHA256       [32]byte
	ExtentOffset uint64
	ExtentSize   uint64
	Codec        uint8
	CodecFlags   uint8
	SrcPath      string
}

func PackSuper(magic []byte, headerSize, headerVer uint16, tocOffset, tocSize uint64, tocSHA [32]byte, blobOffset, fileSize uint64, pkgCount uint32, flags uint32, uuid [16]byte) []byte {
	buf := make([]byte, HeaderSize)
	copy(buf[0:4], magic)
	binary.LittleEndian.PutUint16(buf[0x04:0x06], headerSize)
	binary.LittleEndian.PutUint16(buf[0x06:0x08], headerVer)
	binary.LittleEndian.PutUint64(buf[0x08:0x10], tocOffset)
	binary.LittleEndian.PutUint64(buf[0x10:0x18], tocSize)
	copy(buf[0x18:0x38], tocSHA[:])
	binary.LittleEndian.PutUint64(buf[0x38:0x40], blobOffset)
	binary.LittleEndian.PutUint64(buf[0x40:0x48], fileSize)
	binary.LittleEndian.PutUint32(buf[0x48:0x4C], pkgCount)
	binary.LittleEndian.PutUint32(buf[0x4C:0x50], flags)
	copy(buf[0x50:0x60], uuid[:])
	// reserved zero
	return buf
}

func ParseSuper(buf []byte) (map[string]any, error) {
	if len(buf) < HeaderSize {
		return nil, errors.New("superblock truncated")
	}
	magic := buf[0:4]
	if !bytes.Equal(magic[0:3], Magic[0:3]) {
		return nil, ErrBadMagic
	}
	if magic[3] != 1 {
		return nil, ErrBadVer
	}
	headerSize := binary.LittleEndian.Uint16(buf[0x04:0x06])
	headerVer := binary.LittleEndian.Uint16(buf[0x06:0x08])
	if headerSize < HeaderSize {
		return nil, errors.New("bad header size")
	}
	tocOffset := binary.LittleEndian.Uint64(buf[0x08:0x10])
	tocSize := binary.LittleEndian.Uint64(buf[0x10:0x18])
	var tocSHA [32]byte
	copy(tocSHA[:], buf[0x18:0x38])
	blobOffset := binary.LittleEndian.Uint64(buf[0x38:0x40])
	fileSize := binary.LittleEndian.Uint64(buf[0x40:0x48])
	pkgCount := binary.LittleEndian.Uint32(buf[0x48:0x4C])
	flags := binary.LittleEndian.Uint32(buf[0x4C:0x50])
	var uuid [16]byte
	copy(uuid[:], buf[0x50:0x60])
	for i := 0x60; i < 0x80; i++ {
		if buf[i] != 0 {
			return nil, errors.New("superblock reserved not zero")
		}
	}
	if flags != 0 {
		return nil, errors.New("unknown superblock flags")
	}
	res := map[string]any{
		"header_size":    headerSize,
		"header_version": headerVer,
		"toc_offset":     tocOffset,
		"toc_size":       tocSize,
		"toc_sha256":     tocSHA,
		"blob_offset":    blobOffset,
		"file_size":      fileSize,
		"package_count":  pkgCount,
		"flags":          flags,
		"uuid":           uuid,
	}
	return res, nil
}

func PackPkg(p Package) []byte {
	buf := make([]byte, PkgSize)
	binary.LittleEndian.PutUint64(buf[0x00:0x08], p.TitleID)
	binary.LittleEndian.PutUint32(buf[0x08:0x0C], p.RootDir)
	binary.LittleEndian.PutUint32(buf[0x0C:0x10], p.ExeFSDir)
	binary.LittleEndian.PutUint32(buf[0x10:0x14], p.RomFSDir)
	binary.LittleEndian.PutUint32(buf[0x14:0x18], p.Version)
	buf[0x18] = p.Kind
	buf[0x19] = p.Flags
	binary.LittleEndian.PutUint64(buf[0x20:0x28], p.MetaOffset)
	binary.LittleEndian.PutUint32(buf[0x28:0x2C], p.MetaSize)
	return buf
}

func PackDir(d DirNode) []byte {
	buf := make([]byte, DirSize)
	binary.LittleEndian.PutUint32(buf[0x00:0x04], 0) // name off patched at write
	binary.LittleEndian.PutUint32(buf[0x04:0x08], uint32(len(d.Name)))
	binary.LittleEndian.PutUint32(buf[0x08:0x0C], d.FirstChild)
	binary.LittleEndian.PutUint32(buf[0x0C:0x10], d.ChildCount)
	return buf
}

func PackFile(f FileNode) []byte {
	buf := make([]byte, FileSize)
	binary.LittleEndian.PutUint32(buf[0x00:0x04], 0)
	binary.LittleEndian.PutUint32(buf[0x04:0x08], uint32(len(f.Name)))
	binary.LittleEndian.PutUint64(buf[0x08:0x10], f.Size)
	binary.LittleEndian.PutUint64(buf[0x10:0x18], f.ExtentOffset)
	binary.LittleEndian.PutUint64(buf[0x18:0x20], f.ExtentSize)
	copy(buf[0x20:0x40], f.SHA256[:])
	buf[0x40] = f.Codec
	buf[0x41] = f.CodecFlags
	return buf
}

func ParsePkg(buf []byte, off int) (Package, error) {
	if off+PkgSize > len(buf) {
		return Package{}, errors.New("pkg truncated")
	}
	titleID := binary.LittleEndian.Uint64(buf[off+0x00 : off+0x08])
	root := binary.LittleEndian.Uint32(buf[off+0x08 : off+0x0C])
	exefs := binary.LittleEndian.Uint32(buf[off+0x0C : off+0x10])
	romfs := binary.LittleEndian.Uint32(buf[off+0x10 : off+0x14])
	version := binary.LittleEndian.Uint32(buf[off+0x14 : off+0x18])
	kind := buf[off+0x18]
	flags := buf[off+0x19]
	if buf[off+0x1A] != 0 || buf[off+0x1B] != 0 {
		return Package{}, errors.New("pkg reserved not zero")
	}
	if binary.LittleEndian.Uint32(buf[off+0x1C:off+0x20]) != 0 {
		return Package{}, errors.New("pkg reserved not zero")
	}
	if kind >= uint8(len(KindNames)) && kind != KindAddonUpdate && kind != KindSystem {
		// but spec says >=5 reserved; just allow known
	}
	metaOff := binary.LittleEndian.Uint64(buf[off+0x20 : off+0x28])
	metaSize := binary.LittleEndian.Uint32(buf[off+0x28 : off+0x2C])
	for i := 0x2C; i < 0x40; i++ {
		if buf[off+i] != 0 {
			return Package{}, errors.New("pkg reserved not zero")
		}
	}
	if _, ok := KindNames[kind]; !ok && kind < 5 {
		// ok
	} else if !ok {
		return Package{}, errors.New("unknown package kind")
	}
	return Package{
		TitleID:    titleID,
		RootDir:    root,
		ExeFSDir:   exefs,
		RomFSDir:   romfs,
		Version:    version,
		Kind:       kind,
		Flags:      flags,
		MetaOffset: metaOff,
		MetaSize:   metaSize,
	}, nil
}
