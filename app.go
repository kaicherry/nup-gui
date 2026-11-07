package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kaicherry/nup/access"
	"github.com/kaicherry/nup/edit"
	"github.com/kaicherry/nup/format"
	"github.com/kaicherry/nup/pack"
	"github.com/kaicherry/nup/read"
	"github.com/kaicherry/nup/verify"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx         context.Context
	operationMu sync.Mutex
	packCancel  context.CancelFunc
	packID      uint64
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called at application startup
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.WindowSetMinSize(ctx, minimumWindowWidth, minimumWindowHeight)
	runtime.WindowSetSize(ctx, initialWindowWidth, initialWindowHeight)
}

// PackOptions for packing
type PackOptions struct {
	Level          int               `json:"level"`
	TitleID        string            `json:"titleId"`
	DisplayVersion string            `json:"displayVersion"`
	Publisher      string            `json:"publisher"`
	Name           string            `json:"name"`
	LangNames      map[string]string `json:"langNames"`
	Icon           string            `json:"icon"`
	LangIcons      map[string]string `json:"langIcons"`
	KeepListings   bool              `json:"keepListings"`
	Quiet          bool              `json:"quiet"`
}

func (a *App) doPackWithProgress(ctx context.Context, source, out string, opts PackOptions) (string, error) {
	return a.doPackContext(ctx, source, out, opts)
}

func (a *App) beginPack() (context.Context, func()) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	a.operationMu.Lock()
	a.packID++
	id := a.packID
	a.packCancel = cancel
	a.operationMu.Unlock()

	return ctx, func() {
		a.operationMu.Lock()
		if a.packID == id {
			a.packCancel = nil
		}
		a.operationMu.Unlock()
		cancel()
	}
}

func (a *App) CancelPack() {
	a.operationMu.Lock()
	cancel := a.packCancel
	a.operationMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) SelectDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Directory",
	})
}

func (a *App) SelectFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select File",
		Filters: []runtime.FileFilter{
			{DisplayName: "NUP Files (*.nup)", Pattern: "*.nup"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

func (a *App) SaveFile() (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save NUP File",
		DefaultFilename: "game.nup",
		Filters: []runtime.FileFilter{
			{DisplayName: "NUP Files (*.nup)", Pattern: "*.nup"},
		},
	})
}

func (a *App) Pack(source, out string, opts PackOptions) (string, error) {
	return a.doPack(source, out, opts)
}

func (a *App) PackWithProgress(source, out string, opts PackOptions) (string, error) {
	ctx, cleanup := a.beginPack()
	defer cleanup()
	return a.doPackWithProgress(ctx, source, out, opts)
}

func (a *App) doPack(source, out string, opts PackOptions) (string, error) {
	return a.doPackContext(context.Background(), source, out, opts)
}

func (a *App) doPackContext(ctx context.Context, source, out string, opts PackOptions) (string, error) {
	var tid *uint64
	if opts.TitleID != "" {
		// parse hex
		var v uint64
		for i := 0; i < len(opts.TitleID); i++ {
			c := opts.TitleID[i]
			var d byte
			if c >= '0' && c <= '9' {
				d = c - '0'
			} else if c >= 'a' && c <= 'f' {
				d = c - 'a' + 10
			} else if c >= 'A' && c <= 'F' {
				d = c - 'A' + 10
			} else {
				continue
			}
			v = v*16 + uint64(d)
		}
		tid = &v
	}
	var disp *string
	if opts.DisplayVersion != "" {
		s := opts.DisplayVersion
		disp = &s
	}
	var pub *string
	if opts.Publisher != "" {
		s := opts.Publisher
		pub = &s
	}
	names := map[string]*string{}
	if opts.Name != "" {
		s := opts.Name
		names[""] = &s
	}
	for k, v := range opts.LangNames {
		if v == "" {
			continue
		}
		s := v
		names[strings.ToLower(k)] = &s
	}
	icons := map[string]*string{}
	if opts.Icon != "" {
		s := opts.Icon
		icons[""] = &s
	}
	for k, v := range opts.LangIcons {
		if v == "" {
			continue
		}
		s := v
		icons[strings.ToLower(k)] = &s
	}
	level := opts.Level
	if level == 0 {
		level = 3
	}
	pkgs, err := pack.PackWithProgressContext(ctx, out, source, level, opts.KeepListings, tid, disp, pub, names, icons,
		func(done, total int) {
			runtime.EventsEmit(a.ctx, "pack-progress", map[string]any{"done": done, "total": total})
		})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, p := range pkgs {
		sb.WriteString(fmt.Sprintf("%s %016X v%d\n", format.KindNames[p.Kind], p.TitleID, p.Version))
	}
	return sb.String(), nil
}

func (a *App) Info(path string) (string, error) {
	r, err := read.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File: %s\n", path))
	sb.WriteString(fmt.Sprintf("Packages: %d\n\n", len(r.Packages)))
	for _, p := range r.Packages {
		sb.WriteString(fmt.Sprintf("[%d] %s %016X v0x%x\n", p.Index, p.KindName(), p.TitleID, p.Version))
	}
	return sb.String(), nil
}

func (a *App) GetArchiveIcon(path string, pkgIndex int) (string, error) {
	r, err := read.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if pkgIndex < 0 || pkgIndex >= len(r.Packages) {
		return "", fmt.Errorf("package index out of range")
	}
	p := r.Packages[pkgIndex]
	if p.MetaSize == 0 {
		return "", nil
	}
	block, err := r.ReadRange(p.MetaOffset, uint64(p.MetaSize))
	if err != nil {
		return "", err
	}
	for pos := 0; pos+8 <= len(block); {
		typeID := binary.LittleEndian.Uint16(block[pos : pos+2])
		size := uint64(binary.LittleEndian.Uint32(block[pos+4 : pos+8]))
		pos += 8
		if size > uint64(len(block)-pos) {
			return "", fmt.Errorf("invalid package metadata")
		}
		payload := block[pos : pos+int(size)]
		if typeID == format.TLVIcon {
			contentType := "image/png"
			if len(payload) >= 3 && payload[0] == 0xFF && payload[1] == 0xD8 && payload[2] == 0xFF {
				contentType = "image/jpeg"
			}
			return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(payload), nil
		}
		pos += int(size)
	}
	return "", nil
}

func (a *App) List(path string) (string, error) {
	r, err := read.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var sb strings.Builder
	for i := range r.FileNames {
		name, size, _, _, _, codec := r.FileInfo(i)
		tag := "raw"
		if codec == format.CodecZSTD {
			tag = "zstd"
		}
		if size == 0 {
			tag = "empty"
		}
		sb.WriteString(fmt.Sprintf("%8d  %12d  %s  %s\n", i, size, tag, string(name)))
	}
	return sb.String(), nil
}

func (a *App) Extract(path, dest, pkg string, subtree string) (string, error) {
	r, err := read.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var p *read.PackageInfo
	if pkg != "" {
		for i := range r.Packages {
			if format.KindNames[r.Packages[i].Kind] == pkg {
				p = &r.Packages[i]
				break
			}
		}
		if p == nil {
			// try index
			var idx int
			fmt.Sscanf(pkg, "%d", &idx)
			if idx >= 0 && idx < len(r.Packages) {
				p = &r.Packages[idx]
			}
		}
	}
	if p == nil && len(r.Packages) > 0 {
		p = &r.Packages[0]
	}
	if p == nil {
		return "", fmt.Errorf("no package")
	}
	srcDir := int(p.RootDir)
	if subtree != "" {
		ref := access.ResolvePath(r, p, subtree)
		if ref != nil && ref.IsDir {
			srcDir = ref.Index
		}
	}
	n, err := access.ExtractTree(r, dest, srcDir)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Extracted %d files to %s", n, dest), nil
}

type EditOptions struct {
	Package        string            `json:"package"`
	Name           string            `json:"name"`
	Publisher      string            `json:"publisher"`
	DisplayVersion string            `json:"displayVersion"`
	Icon           string            `json:"icon"`
	LangNames      map[string]string `json:"langNames"`
	LangIcons      map[string]string `json:"langIcons"`
	ClearName      bool              `json:"clearName"`
	ClearPublisher bool              `json:"clearPublisher"`
	ClearDisplay   bool              `json:"clearDisplay"`
	ClearIcon      bool              `json:"clearIcon"`
}

func (a *App) Edit(path string, opts EditOptions) (string, error) {
	var n, p, d, i *string
	if opts.Name != "" {
		s := opts.Name
		n = &s
	}
	if opts.Publisher != "" {
		s := opts.Publisher
		p = &s
	}
	if opts.DisplayVersion != "" {
		s := opts.DisplayVersion
		d = &s
	}
	if opts.Icon != "" {
		s := opts.Icon
		i = &s
	}
	if err := edit.Edit(path, opts.Package, n, p, d, i, opts.LangNames, opts.LangIcons, opts.ClearName, opts.ClearPublisher, opts.ClearDisplay, opts.ClearIcon); err != nil {
		return "", err
	}
	return "Metadata updated", nil
}

func (a *App) Verify(path string) (string, error) {
	ok, probs := verify.Verify(path)
	if !ok {
		return strings.Join(probs, "\n"), fmt.Errorf("verification failed")
	}
	return "OK", nil
}

func (a *App) GetPackageInfo(path string) (map[string]interface{}, error) {
	r, err := read.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	res := map[string]interface{}{
		"packages": len(r.Packages),
	}
	if len(r.Packages) > 0 {
		p := r.Packages[0]
		res["titleId"] = fmt.Sprintf("%016X", p.TitleID)
		res["kind"] = p.KindName()
		res["version"] = p.Version
		// try to read meta
	}
	return res, nil
}

func (a *App) ResizeWindow(height int) {
	runtime.WindowSetSize(a.ctx, initialWindowWidth, height)
}

func (a *App) ResetWindow() {
	runtime.WindowSetSize(a.ctx, initialWindowWidth, initialWindowHeight)
	runtime.WindowSetMinSize(a.ctx, minimumWindowWidth, minimumWindowHeight)
}

func (a *App) CountFiles(path string) (int, error) {
	count := 0
	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

func (a *App) GetFileSize(path string) (float64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	gb := float64(st.Size()) / (1024 * 1024 * 1024)
	return gb, nil
}

func (a *App) SelectSaveDir() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Output Directory",
	})
}

func (a *App) SelectIconFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Icon",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.png;*.jpg;*.jpeg)", Pattern: "*.png;*.jpg;*.jpeg"},
		},
	})
}
