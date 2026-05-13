package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/renameio/v2"
)

// probeTransferState is the client-side state the probe persists during
// a resumable transfer-up. It lives at
// <UserConfigDir>/mwan/transfers/<id>.json. Persisting the committed
// offset after every chunk lets a later invocation with -resume continue
// from there against the same daemon-side transfer id. Hash continuity
// is reconstructed on resume by re-hashing the source file from byte 0
// to the server-authoritative committed offset, so no hash state is
// stored on disk or shipped over the wire.
type probeTransferState struct {
	TransferID      string `json:"transfer_id"`
	SourcePath      string `json:"source_path"`
	RemotePath      string `json:"remote_path"`
	Label           string `json:"label"`
	TotalSize       int64  `json:"total_size"`
	CommittedOffset int64  `json:"committed_offset"`
}

// probeStateDir returns the directory holding probe-side transfer
// state files. The directory is created with 0700 on first use.
func probeStateDir(ctx context.Context) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		slog.ErrorContext(ctx, "probe state: user config dir", "err", err)
		return "", fmt.Errorf("probe state: user config dir: %w", err)
	}
	dir := filepath.Join(base, "mwan", "transfers")
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		slog.ErrorContext(ctx, "probe state: mkdir", "err", mkErr, "dir", dir)
		return "", fmt.Errorf("probe state: mkdir %s: %w", dir, mkErr)
	}
	return dir, nil
}

// probeStatePath returns the absolute state file path for transferID.
func probeStatePath(ctx context.Context, transferID string) (string, error) {
	dir, err := probeStateDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, transferID+".json"), nil
}

// findProbeStateBySource scans the probe state directory for a state
// file referencing source. Returns ("", nil) when no match exists.
func findProbeStateBySource(ctx context.Context, source string) (string, error) {
	dir, err := probeStateDir(ctx)
	if err != nil {
		return "", err
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return "", nil
		}
		slog.ErrorContext(ctx, "probe state: read dir", "err", readErr, "dir", dir)
		return "", fmt.Errorf("probe state: read dir %s: %w", dir, readErr)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		path := filepath.Join(dir, name)
		st, loadErr := loadProbeState(ctx, path)
		if loadErr != nil {
			slog.WarnContext(ctx, "probe state: skip unreadable", "path", path, "err", loadErr)
			continue
		}
		if st.SourcePath == source {
			return path, nil
		}
	}
	return "", nil
}

// loadProbeState reads and parses a probe state file by absolute path.
func loadProbeState(ctx context.Context, path string) (*probeTransferState, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		slog.ErrorContext(ctx, "probe state: read", "err", err, "path", path)
		return nil, fmt.Errorf("probe state: read %s: %w", path, err)
	}
	var st probeTransferState
	if unmarshalErr := json.Unmarshal(content, &st); unmarshalErr != nil {
		slog.ErrorContext(ctx, "probe state: parse", "err", unmarshalErr, "path", path)
		return nil, fmt.Errorf("probe state: parse %s: %w", path, unmarshalErr)
	}
	return &st, nil
}

// saveProbeState writes the state file atomically with 0600 mode.
func saveProbeState(ctx context.Context, st *probeTransferState) error {
	path, pathErr := probeStatePath(ctx, st.TransferID)
	if pathErr != nil {
		return pathErr
	}
	content, marshalErr := json.MarshalIndent(st, "", "  ")
	if marshalErr != nil {
		slog.ErrorContext(ctx, "probe state: marshal", "err", marshalErr, "transfer_id", st.TransferID)
		return fmt.Errorf("probe state: marshal: %w", marshalErr)
	}
	pending, err := renameio.NewPendingFile(path, renameio.WithStaticPermissions(0o600))
	if err != nil {
		slog.ErrorContext(ctx, "probe state: pending", "err", err, "path", path)
		return fmt.Errorf("probe state: pending %s: %w", path, err)
	}
	defer func() { _ = pending.Cleanup() }()
	if _, writeErr := pending.Write(content); writeErr != nil {
		slog.ErrorContext(ctx, "probe state: write", "err", writeErr, "path", path)
		return fmt.Errorf("probe state: write %s: %w", path, writeErr)
	}
	if closeErr := pending.CloseAtomicallyReplace(); closeErr != nil {
		slog.ErrorContext(ctx, "probe state: rename", "err", closeErr, "path", path)
		return fmt.Errorf("probe state: rename %s: %w", path, closeErr)
	}
	return nil
}
