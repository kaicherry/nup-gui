package format

import "testing"

func TestSuperRoundTrip(t *testing.T) {
	var uuid [16]byte
	for i := range uuid {
		uuid[i] = byte(i)
	}
	var tocSHA [32]byte
	for i := range tocSHA {
		tocSHA[i] = byte(0xA0 + i)
	}
	superb := PackSuper(Magic, HeaderSize, 1, HeaderSize, 128, tocSHA, 256, 1024, 1, 0, uuid)
	got, err := ParseSuper(superb)
	if err != nil {
		t.Fatalf("ParseSuper failed: %v", err)
	}
	if got["header_size"].(uint16) != HeaderSize {
		t.Fatalf("header_size = %v", got["header_size"])
	}
	if got["toc_sha256"].([32]byte) != tocSHA {
		t.Fatalf("toc_sha mismatch")
	}
	if got["file_size"].(uint64) != 1024 {
		t.Fatalf("file_size = %v", got["file_size"])
	}
}

func TestSuperBadMagic(t *testing.T) {
	superb := PackSuper(Magic, HeaderSize, 1, HeaderSize, 128, [32]byte{}, 256, 1024, 1, 0, [16]byte{})
	superb[0] = 'X'
	if _, err := ParseSuper(superb); err == nil {
		t.Fatal("expected bad magic error")
	}
}

func TestPkgRoundTrip(t *testing.T) {
	p := Package{TitleID: 0x040074A01BF12000, RootDir: 0, ExeFSDir: NODir, RomFSDir: 1, Version: 0x10000, Kind: KindBase, Flags: 0, MetaOffset: 512, MetaSize: 64}
	buf := PackPkg(p)
	got, err := ParsePkg(buf, 0)
	if err != nil {
		t.Fatalf("ParsePkg failed: %v", err)
	}
	if got.TitleID != p.TitleID || got.Version != p.Version || got.Kind != p.Kind || got.Flags != p.Flags {
		t.Fatalf("pkg mismatch: %+v", got)
	}
}

func TestPkgReservedNotZero(t *testing.T) {
	buf := PackPkg(Package{})
	buf[0x2C] = 1
	if _, err := ParsePkg(buf, 0); err == nil {
		t.Fatal("expected reserved error")
	}
}

func TestPackDir(t *testing.T) {
	d := DirNode{Name: []byte("romfs"), FirstChild: 0, ChildCount: 2}
	buf := PackDir(d)
	if len(buf) != DirSize {
		t.Fatalf("dir size = %d", len(buf))
	}
}

func TestPackFile(t *testing.T) {
	f := FileNode{Name: []byte("hello.bin"), Size: 5, Codec: CodecRaw, CodecFlags: 0}
	buf := PackFile(f)
	if len(buf) != FileSize {
		t.Fatalf("file size = %d", len(buf))
	}
}
