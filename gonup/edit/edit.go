package edit

import (
	"crypto/sha256"
	"encoding/binary"
	"os"

	"github.com/kaicherry/nup/format"
	"github.com/kaicherry/nup/meta"
	"github.com/kaicherry/nup/read"
)

func resolvePackage(r *read.Reader, pkgSel string) *read.PackageInfo {
	if pkgSel != "" {
		if len(pkgSel) > 0 && pkgSel[0] >= '0' && pkgSel[0] <= '9' {
			var idx int
			for i := 0; i < len(pkgSel); i++ {
				idx = idx*10 + int(pkgSel[i]-'0')
			}
			if idx >= 0 && idx < len(r.Packages) {
				return &r.Packages[idx]
			}
		}
		for i := range r.Packages {
			if format.KindNames[r.Packages[i].Kind] == pkgSel {
				return &r.Packages[i]
			}
		}
	}
	for i := range r.Packages {
		if r.Packages[i].Kind == format.KindBase {
			return &r.Packages[i]
		}
	}
	if len(r.Packages) > 0 {
		return &r.Packages[0]
	}
	return nil
}

func Edit(path string, pkgSel string, name, publisher, displayVersion, icon *string, langNames, langIcons map[string]string, clearName, clearPub, clearDisp, clearIcon bool) error {
	r, err := read.Open(path)
	if err != nil {
		return err
	}
	defer r.Close()
	target := resolvePackage(r, pkgSel)
	if target == nil {
		return os.ErrNotExist
	}
	// read old blocks
	var oldBlocks []map[uint16][]byte
	for i := range r.Packages {
		p := &r.Packages[i]
		if p.MetaSize == 0 {
			oldBlocks = append(oldBlocks, map[uint16][]byte{})
			continue
		}
		block, err := r.ReadRange(p.MetaOffset, uint64(p.MetaSize))
		if err != nil {
			return err
		}
		oldBlocks = append(oldBlocks, meta.ParseBlock(block))
	}
	idx := 0
	for i := range r.Packages {
		if &r.Packages[i] == target {
			idx = i
			break
		}
	}
	oldBlocks[idx] = meta.MergeAppMeta(oldBlocks[idx], name, publisher, displayVersion, icon, langNames, langIcons, clearName, clearPub, clearDisp, clearIcon)
	var serialized [][]byte
	for i := range oldBlocks {
		serialized = append(serialized, meta.SerializeBlock(oldBlocks[i]))
	}
	// compute new tail
	var newTail []byte
	for _, s := range serialized {
		newTail = append(newTail, s...)
	}
	// find blob end
	blobEnd := r.Super["file_size"].(uint64)
	for i := range r.Packages {
		if r.Packages[i].MetaSize > 0 {
			if r.Packages[i].MetaOffset < blobEnd {
				blobEnd = r.Packages[i].MetaOffset
			}
		}
	}
	// new offsets
	var newOffs [][2]uint64 // off,size
	cursor := blobEnd
	for _, s := range serialized {
		if len(s) == 0 {
			newOffs = append(newOffs, [2]uint64{0, 0})
			continue
		}
		newOffs = append(newOffs, [2]uint64{cursor, uint64(len(s))})
		cursor += uint64(len(s))
	}
	newFileSize := cursor
	// update toc
	sup := r.Super
	toc, err := r.ReadRange(sup["toc_offset"].(uint64), sup["toc_size"].(uint64))
	if err != nil {
		return err
	}
	pos := 0x30
	for i := range r.Packages {
		mo := newOffs[i]
		binary.LittleEndian.PutUint64(toc[pos+0x20:pos+0x28], mo[0])
		binary.LittleEndian.PutUint32(toc[pos+0x28:pos+0x2C], uint32(mo[1]))
		pos += format.PkgSize
	}
	tocSHA := sha256.Sum256(toc)
	superb := format.PackSuper(format.Magic, sup["header_size"].(uint16), sup["header_version"].(uint16), sup["toc_offset"].(uint64), sup["toc_size"].(uint64), tocSHA, sup["blob_offset"].(uint64), newFileSize, sup["package_count"].(uint32), sup["flags"].(uint32), sup["uuid"].([16]byte))
	// write tmp
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := out.Write(superb); err != nil {
		return fail(err)
	}
	if _, err := out.Write(toc); err != nil {
		return fail(err)
	}
	// copy from toc end to blobEnd
	copyStart := sup["toc_offset"].(uint64) + sup["toc_size"].(uint64)
	if copyStart < blobEnd {
		if err := r.CopyRangeTo(out, copyStart, blobEnd-copyStart); err != nil {
			return fail(err)
		}
	}
	if _, err := out.Write(newTail); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	check, err := read.Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := check.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
