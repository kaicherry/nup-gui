package discover

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/kaicherry/nup/format"
)

var decRe = regexp.MustCompile(`^\d+$`)
var hexRe = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)

type PackInput struct {
	Kind    uint8
	Src     string
	TitleID uint64
	Version uint32
	Flags   uint8
	NPDM    []byte
	BuildID []byte
}

func looksLikeContent(d string) bool {
	if _, err := os.Stat(filepath.Join(d, "exefs")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(d, "romfs")); err == nil {
		return true
	}
	return false
}

func findNPDM(src string) string {
	for _, cand := range []string{filepath.Join(src, "exefs", "main.npdm"), filepath.Join(src, "main.npdm")} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return ""
}

func readBuildID(src string) []byte {
	for _, cand := range []string{filepath.Join(src, "exefs", "main"), filepath.Join(src, "main")} {
		if st, err := os.Stat(cand); err == nil && st.Size() >= 0x60 {
			b, _ := os.ReadFile(cand)
			if len(b) >= 0x60 {
				if string(b[0:4]) == "NSO0" || string(b[0x10:0x14]) == "NRO0" {
					res := make([]byte, 32)
					copy(res, b[0x40:0x60])
					return res
				}
			}
			return nil
		}
	}
	return nil
}

func parseNPDMProgID(data []byte) uint64 {
	// find ACI0
	for i := 0; i < len(data)-0x18; i++ {
		if string(data[i:i+4]) == "ACI0" {
			// prog id at i+0x10
			var pid uint64
			for j := 0; j < 8; j++ {
				pid |= uint64(data[i+0x10+j]) << (8 * uint(j))
			}
			return pid
		}
	}
	return 0
}

func resolveTitleID(pkg *PackInput, override *uint64) {
	np := findNPDM(pkg.Src)
	if np != "" {
		if b, err := os.ReadFile(np); err == nil {
			pkg.NPDM = b
			pid := parseNPDMProgID(b)
			if pid != 0 {
				pkg.TitleID = pid
			}
		}
	}
	pkg.BuildID = readBuildID(pkg.Src)
	if override != nil {
		pkg.TitleID = *override
	}
}

func collectUpdates(updDir string, baseTitleID uint64, flags uint8) []PackInput {
	var out []PackInput
	ents, _ := os.ReadDir(updDir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if !decRe.MatchString(name) {
			continue
		}
		p := filepath.Join(updDir, name)
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			continue
		}
		if !looksLikeContent(p) {
			continue
		}
		var ver uint32
		// parse dec
		var v uint32
		for i := 0; i < len(name); i++ {
			v = v*10 + uint32(name[i]-'0')
		}
		ver = v
		pkg := PackInput{Kind: format.KindUpdate, Src: p, Version: ver, Flags: flags}
		resolveTitleID(&pkg, nil)
		if pkg.TitleID == 0 {
			pkg.TitleID = baseTitleID
		}
		out = append(out, pkg)
	}
	return out
}

func collectDLCs(dlcDir string) []PackInput {
	var out []PackInput
	ents, _ := os.ReadDir(dlcDir)
	var tnames []string
	for _, e := range ents {
		tnames = append(tnames, e.Name())
	}
	sort.Strings(tnames)
	for _, name := range tnames {
		if !hexRe.MatchString(name) {
			continue
		}
		tdir := filepath.Join(dlcDir, name)
		if st, err := os.Stat(tdir); err != nil || !st.IsDir() {
			continue
		}
		idxs, _ := os.ReadDir(tdir)
		var inames []string
		for _, e := range idxs {
			inames = append(inames, e.Name())
		}
		sort.Strings(inames)
		for _, idx := range inames {
			cdir := filepath.Join(tdir, idx)
			if st, err := os.Stat(cdir); err != nil || !st.IsDir() {
				continue
			}
			if looksLikeContent(cdir) {
				// parse hex title id
				var tid uint64
				for i := 0; i < len(name); i++ {
					var d byte
					c := name[i]
					if c >= '0' && c <= '9' {
						d = c - '0'
					} else if c >= 'a' && c <= 'f' {
						d = c - 'a' + 10
					} else if c >= 'A' && c <= 'F' {
						d = c - 'A' + 10
					}
					tid = tid*16 + uint64(d)
				}
				out = append(out, PackInput{Kind: format.KindAddon, Src: cdir, TitleID: tid})
			}
		}
	}
	return out
}

func Discover(src string, baseTitleID *uint64) ([]PackInput, error) {
	var packages []PackInput
	updDir := filepath.Join(src, "updates")
	hasUpdates := false
	if st, err := os.Stat(updDir); err == nil && st.IsDir() {
		hasUpdates = true
	}
	if st, err := os.Stat(filepath.Join(src, "base")); err == nil && st.IsDir() {
		base := PackInput{Kind: format.KindBase, Src: filepath.Join(src, "base")}
		resolveTitleID(&base, baseTitleID)
		packages = append(packages, base)
		if hasUpdates {
			var bid uint64
			if baseTitleID != nil {
				bid = *baseTitleID
			} else {
				bid = base.TitleID
			}
			packages = append(packages, collectUpdates(updDir, bid, 0)...)
		}
		dlcDir := filepath.Join(src, "dlc")
		if st, err := os.Stat(dlcDir); err == nil && st.IsDir() {
			packages = append(packages, collectDLCs(dlcDir)...)
		}
	} else if hasUpdates {
		packages = append(packages, collectUpdates(updDir, func() uint64 {
			if baseTitleID != nil {
				return *baseTitleID
			}
			return 0
		}(), format.PkgFlagStandaloneUpdate)...)
		dlcDir := filepath.Join(src, "dlc")
		if st, err := os.Stat(dlcDir); err == nil && st.IsDir() {
			packages = append(packages, collectDLCs(dlcDir)...)
		}
	} else if looksLikeContent(src) {
		base := PackInput{Kind: format.KindBase, Src: src}
		resolveTitleID(&base, baseTitleID)
		packages = append(packages, base)
	} else {
		return nil, os.ErrNotExist
	}
	if len(packages) == 0 {
		return nil, os.ErrNotExist
	}
	return packages, nil
}

func IsDec(s string) bool { return decRe.MatchString(s) }
func IsHex(s string) bool { return hexRe.MatchString(s) }

// Tree
const FILE_TAG = 0x80000000

type DirObj struct {
	Name       []byte
	Children   []interface{}
	ID         int
	FirstChild uint32
	ChildCount uint32
}

type FileObj struct {
	Name         []byte
	SrcPath      string
	Size         uint64
	SHA256       [32]byte
	ExtentOffset uint64
	ExtentSize   uint64
	Codec        uint8
	CodecFlags   uint8
}

type Tree struct {
	Artifacts map[string]bool
	Dirs      []*DirObj
	Files     []*FileObj
	Children  []uint32
	Pool      map[string]int
	PoolSize  int
}

func NewTree(artifacts map[string]bool) *Tree {
	if artifacts == nil {
		artifacts = map[string]bool{".listing": true}
	}
	return &Tree{Artifacts: artifacts, Pool: map[string]int{}}
}

func (t *Tree) Intern(name []byte) int {
	k := string(name)
	if v, ok := t.Pool[k]; ok {
		return v
	}
	off := t.PoolSize
	t.Pool[k] = off
	t.PoolSize += len(name)
	return off
}

func (t *Tree) AddTree(src string) *DirObj {
	return t.walk(src, []byte{})
}

func (t *Tree) walk(src string, name []byte) *DirObj {
	node := &DirObj{Name: append([]byte(nil), name...)}
	ents, err := os.ReadDir(src)
	if err != nil {
		return node
	}
	// sort by name
	type e struct {
		name string
		isdir bool
		path string
	}
	var list []e
	for _, ent := range ents {
		list = append(list, e{name: ent.Name(), isdir: ent.IsDir(), path: filepath.Join(src, ent.Name())})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	for _, ent := range list {
		ename := []byte(ent.name)
		if ent.isdir {
			node.Children = append(node.Children, t.walk(ent.path, ename))
		} else {
			if t.Artifacts[ent.name] {
				continue
			}
			node.Children = append(node.Children, &FileObj{Name: ename, SrcPath: ent.path})
		}
	}
	return node
}

func (t *Tree) Assign(roots []*DirObj) {
	var rec func(d *DirObj) int
	rec = func(d *DirObj) int {
		did := len(t.Dirs)
		d.ID = did
		t.Dirs = append(t.Dirs, d)
		slotBase := len(t.Children)
		// reserve
		for i := 0; i < len(d.Children); i++ {
			t.Children = append(t.Children, 0)
		}
		for i, child := range d.Children {
			if fo, ok := child.(*FileObj); ok {
				t.Children[slotBase+i] = FILE_TAG + uint32(len(t.Files))
				t.Files = append(t.Files, fo)
			}
		}
		for i, child := range d.Children {
			if do, ok := child.(*DirObj); ok {
				t.Children[slotBase+i] = uint32(rec(do))
			}
		}
		d.FirstChild = uint32(slotBase)
		d.ChildCount = uint32(len(d.Children))
		return did
	}
	for _, root := range roots {
		rec(root)
	}
}
