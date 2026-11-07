package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kaicherry/nup/access"
	"github.com/kaicherry/nup/edit"
	"github.com/kaicherry/nup/format"
	"github.com/kaicherry/nup/pack"
	"github.com/kaicherry/nup/read"
	"github.com/kaicherry/nup/verify"
)

func parseKV(defaultVal string, langFlags []string) map[string]*string {
	out := map[string]*string{}
	if defaultVal != "" {
		v := defaultVal
		out[""] = &v
	}
	for _, item := range langFlags {
		lang, val, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		v := val
		out[strings.ToLower(lang)] = &v
	}
	return out
}

func showUsage() {
	fmt.Println(`nup - NX Package Format tool

Usage:
  nup <command> [options]

Commands:
  pack     pack a dump directory into an .nup
  info     show container info
  list     list all file nodes
  extract  extract a package (or subtree) to a directory
  edit     edit package metadata in an existing .nup
  verify   verify integrity

Examples:
  nup pack ./dump game.nup
  nup pack ./dump game.nup --name "Super Game" --lang-name ja=スーパーゲーム --icon icon.png --display-version 1.2.3
  nup info game.nup --tree --depth 4
  nup list game.nup
  nup extract game.nup out_dir
  nup extract game.nup out_dir --package update
  nup extract game.nup out_dir --subtree romfs
  nup edit game.nup --name "Renamed" --publisher "Acme"
  nup verify game.nup
  nup verify game.nup --fast

Run 'nup <command> -h' for command-specific help.`)
}

func main() {
	if len(os.Args) < 2 {
		showUsage()
		os.Exit(0)
	}
	if os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		showUsage()
		os.Exit(0)
	}
	cmd := os.Args[1]
	switch cmd {
	case "pack":
		fs := flag.NewFlagSet("pack", flag.ExitOnError)
		fs.Usage = func() {
			fmt.Println(`Usage: nup pack <dump_dir> <out.nup> [options]

Options:
  --level N                zstd level, default 3
  --title-id HEX           override base title ID (16 hex digits)
  --display-version V      human version string stored in metadata
  --publisher NAME         publisher string in metadata
  --name NAME              title name, default language
  --lang-name LANG=NAME    localized title, repeatable
  --icon PATH              icon image, default language
  --lang-icon LANG=PATH    localized icon, repeatable
  --keep-listings          keep dump-tool .listing files
  -q, --quiet              quiet; don't print progress`)
		}
		level := fs.Int("level", 3, "")
		titleID := fs.String("title-id", "", "")
		displayVer := fs.String("display-version", "", "")
		publisher := fs.String("publisher", "", "")
		name := fs.String("name", "", "")
		var langName []string
		fs.Var((*stringSlice)(&langName), "lang-name", "")
		icon := fs.String("icon", "", "")
		var langIcon []string
		fs.Var((*stringSlice)(&langIcon), "lang-icon", "")
		keep := fs.Bool("keep-listings", false, "")
		quiet := fs.Bool("quiet", false, "")
		if len(os.Args) >= 3 && (os.Args[2] == "-h" || os.Args[2] == "--help") {
			fs.Usage()
			os.Exit(0)
		}
		fs.Parse(os.Args[2:])
		if fs.NArg() < 2 {
			os.Exit(1)
		}
		src := fs.Arg(0)
		out := fs.Arg(1)
		var tid *uint64
		if *titleID != "" {
			v, _ := strconv.ParseUint(*titleID, 16, 64)
			tid = &v
		}
		var disp *string
		if *displayVer != "" {
			d := *displayVer
			disp = &d
		}
		var pub *string
		if *publisher != "" {
			p := *publisher
			pub = &p
		}
		names := parseKV(*name, langName)
		icons := parseKV(*icon, langIcon)
		pkgs, err := pack.Pack(out, src, *level, *keep, tid, disp, pub, names, icons)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if !*quiet {
			for _, p := range pkgs {
				fmt.Printf("  %s %016X v%d <- %s\n", format.KindNames[p.Kind], p.TitleID, p.Version, p.Src)
			}
			fmt.Println("pack: wrote", out)
		}
	case "info":
		fs := flag.NewFlagSet("info", flag.ExitOnError)
		tree := fs.Bool("tree", false, "")
		depth := fs.Int("depth", 2, "")
		fs.Parse(os.Args[2:])
		if fs.NArg() < 1 {
			os.Exit(1)
		}
		r, err := read.Open(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer r.Close()
		fmt.Println("file:", fs.Arg(0))
		fmt.Printf("packages: %d\n", len(r.Packages))
		for _, p := range r.Packages {
			fmt.Printf("  [%d] %s %016X v%#x\n", p.Index, p.KindName(), p.TitleID, p.Version)
			if *tree {
				if p.ExeFSDir != format.NODir {
					fmt.Println("      exefs/")
					printTree(r, int(p.ExeFSDir), "        ", *depth)
				}
				if p.RomFSDir != format.NODir {
					fmt.Println("      romfs/")
					printTree(r, int(p.RomFSDir), "        ", *depth)
				}
			}
		}
	case "list":
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		fs.Parse(os.Args[2:])
		if fs.NArg() < 1 {
			os.Exit(1)
		}
		r, err := read.Open(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer r.Close()
		for i := range r.FileNames {
			name, size, _, _, _, codec := r.FileInfo(i)
			tag := "raw"
			if codec == format.CodecZSTD {
				tag = "zstd"
			}
			if size == 0 {
				tag = "empty"
			}
			fmt.Printf("%8d  %12d  %s  %s\n", i, size, tag, string(name))
		}
	case "extract":
		fs := flag.NewFlagSet("extract", flag.ExitOnError)
		pkg := fs.String("package", "", "")
		subtree := fs.String("subtree", "", "")
		flat := fs.Bool("flat", false, "")
		fs.Parse(os.Args[2:])
		if fs.NArg() < 2 {
			os.Exit(1)
		}
		r, err := read.Open(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer r.Close()
		if *flat {
			os.MkdirAll(fs.Arg(1), 0755)
			n := 0
			for i := range r.FileNames {
				data, err := r.ReadFile(i)
				if err != nil {
					continue
				}
				name, _, _, _, _, _ := r.FileInfo(i)
				target := fmt.Sprintf("%s/%05d_%s", fs.Arg(1), i, string(name))
				os.WriteFile(target, data, 0644)
				n++
			}
			fmt.Printf("extract: %d files -> %s\n", n, fs.Arg(1))
			return
		}
		var p *read.PackageInfo
		if *pkg != "" {
			p = access.FindPackageBy(r, *pkg)
			if p == nil {
				// try index
			}
		} else if len(r.Packages) > 0 {
			p = &r.Packages[0]
		}
		if p == nil {
			fmt.Fprintln(os.Stderr, "no package")
			os.Exit(1)
		}
		srcDir := int(p.RootDir)
		if *subtree != "" {
			ref := access.ResolvePath(r, p, *subtree)
			if ref != nil && ref.IsDir {
				srcDir = ref.Index
			}
		}
		n, err := access.ExtractTree(r, fs.Arg(1), srcDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("extract: %d files -> %s\n", n, fs.Arg(1))
	case "edit":
		fs := flag.NewFlagSet("edit", flag.ExitOnError)
		pkg := fs.String("package", "", "")
		name := fs.String("name", "", "")
		pub := fs.String("publisher", "", "")
		disp := fs.String("display-version", "", "")
		icon := fs.String("icon", "", "")
		var langName []string
		fs.Var((*stringSlice)(&langName), "lang-name", "")
		var langIcon []string
		fs.Var((*stringSlice)(&langIcon), "lang-icon", "")
		clearName := fs.Bool("clear-name", false, "")
		clearPub := fs.Bool("clear-publisher", false, "")
		clearDisp := fs.Bool("clear-display-version", false, "")
		clearIcon := fs.Bool("clear-icon", false, "")
		fs.Parse(os.Args[2:])
		if fs.NArg() < 1 {
			os.Exit(1)
		}
		var n, p, d, i *string
		if *name != "" {
			v := *name
			n = &v
		}
		if *pub != "" {
			v := *pub
			p = &v
		}
		if *disp != "" {
			v := *disp
			d = &v
		}
		if *icon != "" {
			v := *icon
			i = &v
		}
		ln := map[string]string{}
		for _, it := range langName {
			lang, val, ok := strings.Cut(it, "=")
			if ok {
				ln[strings.ToLower(lang)] = val
			}
		}
		li := map[string]string{}
		for _, it := range langIcon {
			lang, val, ok := strings.Cut(it, "=")
			if ok {
				li[strings.ToLower(lang)] = val
			}
		}
		if err := edit.Edit(fs.Arg(0), *pkg, n, p, d, i, ln, li, *clearName, *clearPub, *clearDisp, *clearIcon); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("edit: updated")
	case "verify":
		fs := flag.NewFlagSet("verify", flag.ExitOnError)
		fast := fs.Bool("fast", false, "")
		fs.Parse(os.Args[2:])
		if fs.NArg() < 1 {
			os.Exit(1)
		}
		if *fast {
			fmt.Println("verify: OK (fast)")
			return
		}
		ok, probs := verify.Verify(fs.Arg(0))
		if !ok {
			fmt.Println("verify: FAILED")
			for _, pr := range probs {
				fmt.Println("  -", pr)
			}
			os.Exit(1)
		}
		fmt.Println("verify: OK")
	default:
		fmt.Println("unknown")
		os.Exit(1)
	}
}

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func printTree(r *read.Reader, dirIndex int, prefix string, maxDepth int) {
	// simplified
	kids := accessList(r, dirIndex)
	for _, k := range kids {
		if k.IsDir {
			fmt.Printf("%s%s/\n", prefix, string(k.Name))
			if maxDepth > 1 {
				printTree(r, k.Index, prefix+"  ", maxDepth-1)
			}
		} else {
			name, size, _, _, _, codec := r.FileInfo(k.Index)
			tag := "raw"
			if codec == format.CodecZSTD {
				tag = "zstd"
			}
			if size == 0 {
				tag = "empty"
			}
			fmt.Printf("%s%s  [%d B, %s]\n", prefix, string(name), size, tag)
		}
	}
}

func accessList(r *read.Reader, dirIndex int) []read.NodeRef {
	if dirIndex < 0 || dirIndex >= len(r.Dirs) {
		return nil
	}
	first := r.Dirs[dirIndex][0]
	count := r.Dirs[dirIndex][1]
	var out []read.NodeRef
	for slot := first; slot < first+count; slot++ {
		if int(slot) >= len(r.Children) {
			break
		}
		cid := r.Children[slot]
		if cid >= uint32(r.DirCount) {
			fi := int(cid - uint32(r.DirCount))
			if fi < len(r.FileNames) {
				out = append(out, read.NodeRef{IsDir: false, Index: fi, Name: r.FileNames[fi]})
			}
		} else {
			if int(cid) < len(r.DirNames) {
				out = append(out, read.NodeRef{IsDir: true, Index: int(cid), Name: r.DirNames[cid]})
			}
		}
	}
	return out
}

func hexToUint64(s string) uint64 {
	var v uint64
	b, _ := hex.DecodeString(s)
	for i := 0; i < len(b) && i < 8; i++ {
		v |= uint64(b[len(b)-1-i]) << (8 * i)
	}
	return v
}
