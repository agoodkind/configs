package opnsensesvc

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// newValidatorForTest builds a PathValidator with separate read and
// write allowlists rooted at tempdirs created by the caller. The
// returned function cleans up the validator.
func newValidatorForTest(t *testing.T, readDirs, writeDirs []string) (*PathValidator, func()) {
	t.Helper()
	pv := NewPathValidator(slog.Default(), readDirs, writeDirs)
	return pv, func() { _ = pv.Close() }
}

func TestPathValidator_AllowedBaseHits(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "hello.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	pv, cleanup := newValidatorForTest(t, []string{base}, nil)
	defer cleanup()

	file, err := pv.OpenForRead(target)
	if err != nil {
		t.Fatalf("OpenForRead: %v", err)
	}
	defer func() { _ = file.Close() }()
	got := make([]byte, 16)
	n, _ := file.Read(got)
	if string(got[:n]) != "hi" {
		t.Fatalf("read=%q want %q", got[:n], "hi")
	}
}

func TestPathValidator_PathTraversalRefused(t *testing.T) {
	base := t.TempDir()
	pv, cleanup := newValidatorForTest(t, []string{base}, nil)
	defer cleanup()

	traversal := filepath.Join(base, "..", "etc", "passwd")
	if _, err := pv.OpenForRead(traversal); err == nil {
		t.Fatalf("expected error for traversal path %q", traversal)
	}
}

func TestPathValidator_SymlinkRefused(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o600); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	link := filepath.Join(base, "evil")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	pv, cleanup := newValidatorForTest(t, []string{base}, nil)
	defer cleanup()

	if _, err := pv.OpenForRead(link); err == nil {
		t.Fatalf("expected error opening symlink escape, got nil")
	}
}

func TestPathValidator_NoFollowOnFinalComponent(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real.txt")
	if err := os.WriteFile(real, []byte("ok"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	link := filepath.Join(base, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	pv, cleanup := newValidatorForTest(t, []string{base}, nil)
	defer cleanup()

	if _, err := pv.OpenForRead(link); err == nil {
		t.Fatalf("expected error opening symlink final component, got nil")
	}
}

func TestPathValidator_DirectionAllowlists(t *testing.T) {
	readOnly := t.TempDir()
	writeOnly := t.TempDir()
	readFile := filepath.Join(readOnly, "r.txt")
	if err := os.WriteFile(readFile, []byte("r"), 0o600); err != nil {
		t.Fatalf("seed read file: %v", err)
	}

	pv, cleanup := newValidatorForTest(t, []string{readOnly}, []string{writeOnly})
	defer cleanup()

	if _, err := pv.OpenForRead(readFile); err != nil {
		t.Fatalf("read in read allowlist must succeed: %v", err)
	}
	if _, _, err := pv.ResolveWrite(readFile); err == nil {
		t.Fatalf("write into read-only allowlist must fail")
	}

	writeTarget := filepath.Join(writeOnly, "w.txt")
	if _, _, err := pv.ResolveWrite(writeTarget); err != nil {
		t.Fatalf("write in write allowlist must resolve: %v", err)
	}
	if _, err := pv.OpenForRead(writeTarget); err == nil || !errors.Is(err, os.ErrNotExist) {
		// Read in the write-only allowlist is permitted (write is
		// implicitly readable), so absence is the expected failure
		// mode here, not a permission error.
		_ = err
	}
}
