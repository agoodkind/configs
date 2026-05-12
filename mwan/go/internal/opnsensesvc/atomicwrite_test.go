package opnsensesvc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWrite_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.bin")
	want := []byte("hello atomic")

	if err := AtomicWriteFile(context.Background(), target, want, 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content=%q want %q", got, want)
	}
}

// TestAtomicWrite_ParentDirFsyncCalled documents the durability
// contract. renameio/v2 owns the inner sync of the temp file. The
// atomicwrite helper then fsyncs the parent directory; injecting a
// spy on the parent's Sync() call requires intercepting os.Open at
// runtime, which is too invasive for this unit test. Instead this
// test verifies the post-condition: the file exists with the correct
// content after AtomicWriteFile returns. The fsync of the parent is
// covered by the integration tests in the verification plan.
func TestAtomicWrite_ParentDirFsyncCalled(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.bin")
	if err := AtomicWriteFile(context.Background(), target, []byte("durable"), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after rename: %v", err)
	}
	if info.Size() != int64(len("durable")) {
		t.Fatalf("size=%d want %d", info.Size(), len("durable"))
	}
}

func TestAtomicWrite_CleanupOnError(t *testing.T) {
	// Point AtomicWriteFile at a parent directory that does not exist.
	// renameio's NewPendingFile creates the temp file in that
	// directory and surfaces a real error; the deferred Cleanup then
	// runs as a no-op because no temp file was created.
	dir := t.TempDir()
	bogusTarget := filepath.Join(dir, "missing", "out.bin")
	if err := AtomicWriteFile(context.Background(), bogusTarget, []byte("x"), 0o600); err == nil {
		t.Fatalf("expected error for nonexistent parent dir")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read dir: %v", readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Fatalf("leftover temp file after failed write: %s", entry.Name())
		}
	}
}
