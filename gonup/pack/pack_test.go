package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaicherry/nup/read"
	"github.com/kaicherry/nup/verify"
)

// cancellationTestCompressionLevel is the fixture's compression level 3.
// Zero would exercise the packer's default instead.
const cancellationTestCompressionLevel = 3

func TestPackAndVerifyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	exefs := filepath.Join(dir, "exefs")
	romfs := filepath.Join(dir, "romfs")
	if err := os.MkdirAll(exefs, 0755); err != nil {
		return
	}
	if err := os.MkdirAll(romfs, 0755); err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(exefs, "main.npdm"), []byte("ACI0\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), 0644); err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(romfs, "hello.txt"), []byte("hello nup"), 0644); err != nil {
		return
	}

	out := filepath.Join(t.TempDir(), "game.nup")
	pkgs, err := Pack(out, dir, 3, false, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	ok, problems := verify.Verify(out)
	if !ok {
		t.Fatalf("verify failed: %v", problems)
	}
	r, err := read.Open(out)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer r.Close()
	if len(r.Packages) != 1 {
		t.Fatalf("expected 1 package in reader, got %d", len(r.Packages))
	}
}

func TestPackWithProgressContextCancellationRemovesPartialOutput(t *testing.T) {
	src := t.TempDir()
	exefs := filepath.Join(src, "exefs")
	romfs := filepath.Join(src, "romfs")
	if err := os.MkdirAll(exefs, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(romfs, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exefs, "main.npdm"), []byte("ACI0\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(romfs, name), []byte(name), os.ModePerm); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(t.TempDir(), "cancelled.nup")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := PackWithProgressContext(ctx, out, src, cancellationTestCompressionLevel, false, nil, nil, nil, nil, nil, func(done, total int) {
		if done < total {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected partial output to be removed, stat error: %v", err)
	}
}
