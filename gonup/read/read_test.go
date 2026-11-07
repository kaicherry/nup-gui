package read_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kaicherry/nup/edit"
	"github.com/kaicherry/nup/pack"
	"github.com/kaicherry/nup/read"
	"github.com/kaicherry/nup/verify"
)

// archivePayloadSize is the test archive's 32 MiB payload.
// A whole-container copy must exceed the metadata-open budget.
const archivePayloadSize int64 = 32 << 20

// metadataOperationAllocationBudget allows allocations for container metadata.
// It stays well below the payload size to catch eager blob reads.
const metadataOperationAllocationBudget uint64 = 8 << 20

// compressionDisabledLevel keeps the fixture payload as a raw extent.
// Compression would make archive size unsuitable for the allocation check.
const compressionDisabledLevel = -1

func TestOpenDoesNotLoadPayloadIntoMemory(t *testing.T) {
	out := makeLargeArchive(t)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r, err := read.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > metadataOperationAllocationBudget {
		t.Fatalf("Open allocated %d bytes for a metadata-only read; budget is %d", allocated, metadataOperationAllocationBudget)
	}

	var largeIndex int
	foundLargeFile := false
	for i, name := range r.FileNames {
		if string(name) == "large.bin" {
			largeIndex = i
			foundLargeFile = true
			break
		}
	}
	if !foundLargeFile {
		t.Fatal("large payload missing from container")
	}
	data, err := r.ReadFile(largeIndex)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(data)) != uint64(archivePayloadSize) {
		t.Fatalf("ReadFile returned %d bytes, want %d", len(data), archivePayloadSize)
	}
}

func TestEditDoesNotLoadPayloadIntoMemory(t *testing.T) {
	out := makeLargeArchive(t)
	title := "Updated title"
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if err := edit.Edit(out, "", &title, nil, nil, nil, nil, nil, false, false, false, false); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > metadataOperationAllocationBudget {
		t.Fatalf("Edit allocated %d bytes for a metadata update; budget is %d", allocated, metadataOperationAllocationBudget)
	}
	if ok, problems := verify.Verify(out); !ok {
		t.Fatalf("edited container failed verification: %v", problems)
	}
}

func makeLargeArchive(t *testing.T) string {
	t.Helper()
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
	largeFile, err := os.Create(filepath.Join(romfs, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := largeFile.Truncate(archivePayloadSize); err != nil {
		largeFile.Close()
		t.Fatal(err)
	}
	if err := largeFile.Close(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "large.nup")
	if _, err := pack.Pack(out, src, compressionDisabledLevel, false, nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	return out
}
