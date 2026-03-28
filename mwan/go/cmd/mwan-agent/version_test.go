package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func TestBuildVersion_NonEmpty(t *testing.T) {
	t.Parallel()
	v := buildVersion()
	if strings.TrimSpace(v) == "" {
		t.Fatal("buildVersion() empty")
	}
}

func TestBuildVersion_DirtyFlag(t *testing.T) {
	// Temporarily set gitDirty to "dirty" and confirm the -dirty suffix appears.
	orig := gitDirty
	gitDirty = "dirty"
	defer func() { gitDirty = orig }()
	v := buildVersion()
	if !strings.Contains(v, "-dirty") {
		t.Fatalf("buildVersion() with dirty=%q: want -dirty in %q", gitDirty, v)
	}
}

func TestBinaryHash_Form(t *testing.T) {
	t.Parallel()
	h := binaryHash()
	if h == "unknown" {
		return
	}
	if len(h) != 12 {
		t.Fatalf("binaryHash len=%d want 12 or unknown", len(h))
	}
	for _, r := range h {
		if !unicode.Is(unicode.Hex_Digit, r) {
			t.Fatalf("non-hex in binaryHash: %q", h)
		}
	}
}

func TestBinaryHashFrom_KnownFile(t *testing.T) {
	t.Parallel()
	// Write a small known file and hash it.
	p := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := binaryHashFrom(p)
	if len(h) != 12 {
		t.Fatalf("got %q want 12-char hex", h)
	}
}

func TestBinaryHashFrom_MissingFile(t *testing.T) {
	t.Parallel()
	h := binaryHashFrom(filepath.Join(t.TempDir(), "nope"))
	if h != "unknown" {
		t.Fatalf("got %q want unknown", h)
	}
}

func TestBinaryHashFrom_Directory(t *testing.T) {
	t.Parallel()
	// Passing a directory as path: os.Open succeeds but io.Copy on a directory
	// returns an error on Linux (is a directory), returns "unknown".
	// On some platforms this might succeed; we just check it doesn't panic.
	_ = binaryHashFrom(t.TempDir())
}

func TestBuildVersionString_ContainsMarkers(t *testing.T) {
	t.Parallel()
	s := buildVersionString()
	if !strings.Contains(s, "commit=") {
		t.Fatalf("missing commit=: %q", s)
	}
	if !strings.Contains(s, "binhash=") {
		t.Fatalf("missing binhash=: %q", s)
	}
}
