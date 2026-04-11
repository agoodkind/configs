package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// These are injected at build time via:
//
//	go build -ldflags="-X goodkind.io/mwan/internal/version.gitCommit=$(git rev-parse --short HEAD) \
//	                    -X goodkind.io/mwan/internal/version.gitDirty=$(git diff --quiet HEAD -- . && echo clean || echo dirty)"
//
// When building without ldflags (e.g. go run or plain go build) they stay
// as the empty-string sentinel values below.
var (
	gitCommit = ""
	gitDirty  = ""
)

// BuildVersion returns a human-readable build fingerprint of the form:
//
//	<commit>[-dirty]+<binhash>
//
// where <binhash> is the first 12 hex characters of SHA-256 of the running
// binary. This ensures that even an uncommitted build gets a stable,
// log-searchable identifier that matches the file on disk.
func BuildVersion() string {
	commit := gitCommit
	if commit == "" {
		commit = "unknown"
	}
	dirty := gitDirty
	if dirty == "" {
		dirty = "unknown"
	}

	binHash := BinaryHash()

	var sb strings.Builder
	sb.WriteString(commit)
	if dirty == "dirty" {
		sb.WriteString("-dirty")
	}
	sb.WriteString("+")
	sb.WriteString(binHash)
	return sb.String()
}

// BinaryHash returns the first 12 hex characters of SHA-256 of the running
// binary. Returns "unknown" on any error.
func BinaryHash() string {
	return binaryHashFrom("")
}

// binaryHashFrom hashes the file at path. If path is empty it falls back to
// os.Executable(). Not exported (used internally and by tests).
func binaryHashFrom(path string) string {
	if path == "" {
		var err error
		path, err = os.Executable()
		if err != nil {
			return "unknown"
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return "unknown"
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// BuildVersionString returns a full one-line summary for startup logs.
func BuildVersionString() string {
	commit := gitCommit
	if commit == "" {
		commit = "unknown"
	}
	dirty := gitDirty
	if dirty == "" {
		dirty = "unknown"
	}
	return fmt.Sprintf("commit=%s dirty=%s binhash=%s", commit, dirty, BinaryHash())
}
