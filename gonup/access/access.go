package access

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaicherry/nup/format"
	"github.com/kaicherry/nup/read"
)

func FindPackage(r *read.Reader, kind *uint8) *read.PackageInfo {
	if kind != nil {
		for i := range r.Packages {
			if r.Packages[i].Kind == *kind {
				return &r.Packages[i]
			}
		}
		return nil
	}
	if len(r.Packages) > 0 {
		return &r.Packages[0]
	}
	return nil
}

func iterChildrenReal(r *read.Reader, dirIndex int) []read.NodeRef {
	if dirIndex < 0 || dirIndex >= len(r.Dirs) {
		return nil
	}
	first := r.Dirs[dirIndex][0]
	count := r.Dirs[dirIndex][1]
	out := []read.NodeRef{}
	for slot := first; slot < first+count; slot++ {
		if int(slot) >= len(r.Children) {
			break
		}
		cid := r.Children[slot]
		if cid >= uint32(r.DirCount) {
			fi := int(cid - uint32(r.DirCount))
			if fi >= len(r.FileNames) {
				continue
			}
			out = append(out, read.NodeRef{IsDir: false, Index: fi, Name: r.FileNames[fi]})
		} else {
			if int(cid) >= len(r.DirNames) {
				continue
			}
			out = append(out, read.NodeRef{IsDir: true, Index: int(cid), Name: r.DirNames[cid]})
		}
	}
	return out
}

func FindChild(r *read.Reader, dirIndex int, name []byte) *read.NodeRef {
	kids := iterChildrenReal(r, dirIndex)
	// binary search simple
	lo, hi := 0, len(kids)
	for lo < hi {
		mid := (lo + hi) / 2
		if string(kids[mid].Name) < string(name) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(kids) && string(kids[lo].Name) == string(name) {
		ref := kids[lo]
		return &ref
	}
	return nil
}

func ResolvePath(r *read.Reader, pkg *read.PackageInfo, relpath string) *read.NodeRef {
	cur := read.NodeRef{IsDir: true, Index: int(pkg.RootDir)}
	parts := strings.Split(relpath, "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !cur.IsDir {
			return nil
		}
		nxt := FindChild(r, cur.Index, []byte(part))
		if nxt == nil {
			return nil
		}
		cur = *nxt
	}
	return &cur
}

func ExtractTree(r *read.Reader, dest string, dirIndex int) (int, error) {
	n := 0
	os.MkdirAll(dest, 0755)
	type item struct {
		di int
		dp string
	}
	stack := []item{{di: dirIndex, dp: dest}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		os.MkdirAll(it.dp, 0755)
		for _, child := range iterChildrenReal(r, it.di) {
			cpath := filepath.Join(it.dp, string(child.Name))
			if child.IsDir {
				stack = append(stack, item{di: child.Index, dp: cpath})
			} else {
				data, err := r.ReadFile(child.Index)
				if err != nil {
					return n, err
				}
				if err := os.WriteFile(cpath, data, 0644); err != nil {
					return n, err
				}
				n++
				if n%100 == 0 {
					fmt.Fprintf(os.Stderr, "\rextract: %d files", n)
				}
			}
		}
	}
	if n > 0 {
		fmt.Fprintln(os.Stderr)
	}
	return n, nil
}

func FindPackageBy(r *read.Reader, kindName string) *read.PackageInfo {
	for i := range r.Packages {
		if format.KindNames[r.Packages[i].Kind] == kindName {
			return &r.Packages[i]
		}
	}
	// try index
	return nil
}
