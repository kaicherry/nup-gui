package pack

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kaicherry/nup/discover"
	"github.com/kaicherry/nup/format"
	"github.com/kaicherry/nup/meta"
	"github.com/klauspost/compress/zstd"
)

func locateContentDirs(packages []discover.PackInput, roots []*discover.DirObj) []format.Package {
	var pkgRecords []format.Package
	for i, pkg := range packages {
		if i >= len(roots) {
			continue
		}
		root := roots[i]
		rec := format.Package{TitleID: pkg.TitleID, RootDir: uint32(root.ID), Version: pkg.Version, Kind: pkg.Kind, Flags: pkg.Flags}
		for _, child := range root.Children {
			if d, ok := child.(*discover.DirObj); ok {
				if string(d.Name) == "exefs" {
					rec.ExeFSDir = uint32(d.ID)
				} else if string(d.Name) == "romfs" {
					rec.RomFSDir = uint32(d.ID)
				}
			}
		}
		pkgRecords = append(pkgRecords, rec)
	}
	return pkgRecords
}

func deriveUUID(pkgRecords []format.Package) [16]byte {
	h := sha256.New()
	for _, rec := range pkgRecords {
		var buf [1 + 4 + 8]byte
		binary.LittleEndian.PutUint64(buf[0:8], rec.TitleID)
		binary.LittleEndian.PutUint32(buf[8:12], rec.Version)
		buf[12] = rec.Kind
		h.Write(buf[:13])
	}
	sum := h.Sum(nil)
	var uuid [16]byte
	copy(uuid[:], sum[:16])
	return uuid
}

type ProgressFn func(done, total int)

func Pack(outPath string, src string, level int, keepListings bool, baseTitleID *uint64, displayVersion, publisher *string, names map[string]*string, icons map[string]*string) ([]discover.PackInput, error) {
	return PackWithProgress(outPath, src, level, keepListings, baseTitleID, displayVersion, publisher, names, icons, nil)
}

func PackWithProgress(outPath string, src string, level int, keepListings bool, baseTitleID *uint64, displayVersion, publisher *string, names map[string]*string, icons map[string]*string, onProgress ProgressFn) ([]discover.PackInput, error) {
	return PackWithProgressContext(context.Background(), outPath, src, level, keepListings, baseTitleID, displayVersion, publisher, names, icons, onProgress)
}

func PackWithProgressContext(ctx context.Context, outPath string, src string, level int, keepListings bool, baseTitleID *uint64, displayVersion, publisher *string, names map[string]*string, icons map[string]*string, onProgress ProgressFn) ([]discover.PackInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(outPath); err == nil {
		return nil, os.ErrExist
	}
	packages, err := discover.Discover(src, baseTitleID)
	if err != nil {
		return nil, err
	}
	arts := map[string]bool{".listing": true}
	if keepListings {
		arts = map[string]bool{}
	}
	tree := discover.NewTree(arts)
	var roots []*discover.DirObj
	for _, pkg := range packages {
		roots = append(roots, tree.AddTree(pkg.Src))
	}
	tree.Assign(roots)
	// intern
	for _, d := range tree.Dirs {
		tree.Intern(d.Name)
	}
	for _, f := range tree.Files {
		tree.Intern(f.Name)
	}
	pkgRecords := locateContentDirs(packages, roots)
	uuid := deriveUUID(pkgRecords)
	// toc size
	dirCount := len(tree.Dirs)
	fileCount := len(tree.Files)
	slotCount := len(tree.Children)
	tocSize := 0x30 + len(pkgRecords)*format.PkgSize + dirCount*format.DirSize + fileCount*format.FileSize + slotCount*4 + tree.PoolSize
	blobOffset := (format.HeaderSize + tocSize + format.ExtentAlign - 1) &^ (format.ExtentAlign - 1)
	// write
	f, err := os.Create(outPath)
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			os.Remove(outPath)
		}
	}()
	// placeholder
	if _, err := f.Write(make([]byte, blobOffset)); err != nil {
		return nil, err
	}
	// extents
	var zc *zstd.Encoder
	if level >= 0 {
		if enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level))); err == nil {
			zc = enc
		}
	}
	blobCursor := blobOffset
	seen := map[[32]byte][3]uint64{}
	total := len(tree.Files)
	last := 0
	for i, fn := range tree.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(fn.SrcPath)
		if err != nil {
			return nil, err
		}
		fn.Size = uint64(len(data))
		fn.SHA256 = sha256.Sum256(data)
		if hit, ok := seen[fn.SHA256]; ok && fn.Size > 0 {
			fn.ExtentOffset = hit[0]
			fn.ExtentSize = hit[1]
			fn.Codec = uint8(hit[2])
		} else if fn.Size == 0 {
			fn.ExtentOffset = 0
			fn.ExtentSize = 0
			fn.Codec = format.CodecRaw
		} else {
			payload := data
			codec := uint8(format.CodecRaw)
			if zc != nil {
				comp := zc.EncodeAll(data, nil)
				if len(comp) < len(data) {
					payload = comp
					codec = format.CodecZSTD
				}
			}
			aligned := (blobCursor + format.ExtentAlign - 1) &^ (format.ExtentAlign - 1)
			if aligned > blobCursor {
				f.Write(make([]byte, aligned-blobCursor))
				blobCursor = aligned
			}
			fn.ExtentOffset = uint64(blobCursor)
			fn.ExtentSize = uint64(len(payload))
			fn.Codec = codec
			if _, err := f.Write(payload); err != nil {
				return nil, err
			}
			blobCursor += len(payload)
			seen[fn.SHA256] = [3]uint64{fn.ExtentOffset, fn.ExtentSize, uint64(codec)}
		}
		if total > 0 && (i-last >= 10 || i == total-1) {
			pct := (i + 1) * 100 / total
			fmt.Fprintf(os.Stderr, "pack:progress:%d/%d:%d%%\n", i+1, total, pct)
			fmt.Fprintf(os.Stderr, "\rpack: %d/%d files (%d%%)", i+1, total, pct)
			last = i
		}
		if onProgress != nil {
			onProgress(i+1, total)
		}
	}
	if onProgress != nil && len(tree.Files) > 0 {
		onProgress(int(len(tree.Files)), int(len(tree.Files)))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if total > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "pack:finalizing\n")
		fmt.Fprintf(os.Stderr, "pack: writing metadata and finalizing...\n")
	}
	// meta
	dumpLabel := filepath.Base(src)
	appExtra := meta.AppMeta(names, icons, displayVersion, publisher, dumpLabel)
	// meta blocks
	type mo struct {
		off uint64
		sz  uint32
	}
	var metaOffs []mo
	appPkgIdx := 0
	for i := range packages {
		if packages[i].Kind == format.KindBase {
			appPkgIdx = i
			break
		}
	}
	for i := range packages {
		var aextra []byte
		if i == appPkgIdx {
			aextra = appExtra
		}
		block := meta.MetaBlock(packages[i].NPDM, packages[i].BuildID, dumpLabel, aextra)
		moff := uint64(blobCursor)
		if len(block) > 0 {
			if _, err := f.Write(block); err != nil {
				return nil, err
			}
			blobCursor += len(block)
			metaOffs = append(metaOffs, mo{off: moff, sz: uint32(len(block))})
		} else {
			metaOffs = append(metaOffs, mo{off: 0, sz: 0})
		}
	}
	fileSize := uint64(blobCursor)
	fmt.Fprintf(os.Stderr, "pack:finalizing:toc\n")
	fmt.Fprintf(os.Stderr, "pack: finalizing toc...\n")
	// build toc
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	toc := make([]byte, tocSize)
	pos := 0x30
	for i, rec := range pkgRecords {
		// pack pkg
		binary.LittleEndian.PutUint64(toc[pos+0x00:pos+0x08], rec.TitleID)
		binary.LittleEndian.PutUint32(toc[pos+0x08:pos+0x0C], rec.RootDir)
		binary.LittleEndian.PutUint32(toc[pos+0x0C:pos+0x10], rec.ExeFSDir)
		binary.LittleEndian.PutUint32(toc[pos+0x10:pos+0x14], rec.RomFSDir)
		binary.LittleEndian.PutUint32(toc[pos+0x14:pos+0x18], rec.Version)
		toc[pos+0x18] = rec.Kind
		toc[pos+0x19] = rec.Flags
		binary.LittleEndian.PutUint64(toc[pos+0x20:pos+0x28], metaOffs[i].off)
		binary.LittleEndian.PutUint32(toc[pos+0x28:pos+0x2C], metaOffs[i].sz)
		pos += format.PkgSize
	}
	for _, d := range tree.Dirs {
		off := tree.Intern(d.Name)
		binary.LittleEndian.PutUint32(toc[pos+0x00:pos+0x04], uint32(off))
		binary.LittleEndian.PutUint32(toc[pos+0x04:pos+0x08], uint32(len(d.Name)))
		binary.LittleEndian.PutUint32(toc[pos+0x08:pos+0x0C], d.FirstChild)
		binary.LittleEndian.PutUint32(toc[pos+0x0C:pos+0x10], d.ChildCount)
		pos += format.DirSize
	}
	for _, fn := range tree.Files {
		off := tree.Intern(fn.Name)
		binary.LittleEndian.PutUint32(toc[pos+0x00:pos+0x04], uint32(off))
		binary.LittleEndian.PutUint32(toc[pos+0x04:pos+0x08], uint32(len(fn.Name)))
		binary.LittleEndian.PutUint64(toc[pos+0x08:pos+0x10], fn.Size)
		binary.LittleEndian.PutUint64(toc[pos+0x10:pos+0x18], fn.ExtentOffset)
		binary.LittleEndian.PutUint64(toc[pos+0x18:pos+0x20], fn.ExtentSize)
		copy(toc[pos+0x20:pos+0x40], fn.SHA256[:])
		toc[pos+0x40] = fn.Codec
		pos += format.FileSize
	}
	for _, cid := range tree.Children {
		c := cid
		if c >= discover.FILE_TAG {
			// convert
			cid2 := dirCount + int(c-discover.FILE_TAG)
			binary.LittleEndian.PutUint32(toc[pos:pos+4], uint32(cid2))
		} else {
			binary.LittleEndian.PutUint32(toc[pos:pos+4], c)
		}
		pos += 4
	}
	poolOff := pos
	// write pool
	// build pool in order of offsets
	pool := make([]byte, tree.PoolSize)
	for k, v := range tree.Pool {
		copy(pool[v:v+len(k)], []byte(k))
	}
	copy(toc[poolOff:poolOff+tree.PoolSize], pool)
	// toc header
	copy(toc[0:4], format.TOCMagic)
	binary.LittleEndian.PutUint32(toc[0x04:0x08], uint32(len(pkgRecords)))
	binary.LittleEndian.PutUint64(toc[0x08:0x10], uint64(dirCount))
	binary.LittleEndian.PutUint64(toc[0x10:0x18], uint64(fileCount))
	binary.LittleEndian.PutUint64(toc[0x18:0x20], uint64(poolOff))
	binary.LittleEndian.PutUint64(toc[0x20:0x28], uint64(tree.PoolSize))
	// hash toc
	fmt.Fprintf(os.Stderr, "pack:finalizing:hash\n")
	fmt.Fprintf(os.Stderr, "pack: hashing toc...\n")
	tocSHA := sha256.Sum256(toc)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	superb := format.PackSuper(format.Magic, format.HeaderSize, 1, format.HeaderSize, uint64(tocSize), tocSHA, uint64(blobOffset), fileSize, uint32(len(pkgRecords)), 0, uuid)
	// rewrite
	fmt.Fprintf(os.Stderr, "pack:finalizing:write\n")
	fmt.Fprintf(os.Stderr, "pack: writing superblock...\n")
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	if _, err := f.Write(superb); err != nil {
		return nil, err
	}
	if _, err := f.Write(toc); err != nil {
		return nil, err
	}
	if err := f.Truncate(int64(fileSize)); err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "pack:done\n")
	fmt.Fprintf(os.Stderr, "pack: done\n")
	complete = true
	return packages, nil
}
