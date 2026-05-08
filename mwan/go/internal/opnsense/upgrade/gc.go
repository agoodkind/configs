package upgrade

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// GCResult records the outcome of a single gc invocation. Deleted
// holds the snapshot names that were removed; Skipped holds names that
// were inspected but retained (either younger than the threshold or
// renamed to KeepPrefix).
type GCResult struct {
	Deleted []string
	Skipped []string
}

// GC sweeps upgrade snapshots older than opts.OlderThan. Default is
// DefaultGCThreshold per resolved decision 11.8. The function refuses
// to run without an explicit VMID so it cannot accidentally sweep
// snapshots on an unrelated VM.
func GC(ctx context.Context, deps Deps, opts Options) (GCResult, error) {
	if err := validateOptions(opts); err != nil {
		slog.ErrorContext(ctx, "upgrade.GC: invalid options", "err", err)
		return GCResult{Deleted: nil, Skipped: nil}, err
	}
	if deps.Snap == nil {
		err := errors.New("upgrade.GC: deps.Snap is required")
		slog.ErrorContext(ctx, "upgrade.GC: deps.Snap missing", "err", err)
		return GCResult{Deleted: nil, Skipped: nil}, err
	}
	clk := clockOrDefault(deps.Clock)
	now := clk.Now()
	threshold := opts.OlderThan
	if threshold <= 0 {
		threshold = DefaultGCThreshold
	}

	listing, err := deps.Snap.VMSnapshots(ctx, opts.VMID)
	if err != nil {
		slog.ErrorContext(ctx, "upgrade.GC: VMSnapshots", "err", err, "vmid", opts.VMID)
		return GCResult{Deleted: nil, Skipped: nil}, fmt.Errorf("upgrade.GC: VMSnapshots: %w", err)
	}

	result := GCResult{Deleted: nil, Skipped: nil}
	for _, name := range parseSnapshotNames(listing) {
		if !IsUpgradeSnapshot(name) {
			continue
		}
		ts, ok := timestampFromName(name)
		if !ok {
			continue
		}
		age := now.Sub(time.Unix(ts, 0))
		if age < threshold {
			result.Skipped = append(result.Skipped, name)
			continue
		}
		if err := deps.Snap.VMDelSnapshot(ctx, opts.VMID, name); err != nil {
			slog.WarnContext(ctx, "upgrade.GC: VMDelSnapshot failed", "err", err, "snapshot", name)
			result.Skipped = append(result.Skipped, name)
			continue
		}
		result.Deleted = append(result.Deleted, name)
	}
	slog.InfoContext(ctx, "upgrade.GC: complete", "vmid", opts.VMID,
		"deleted", len(result.Deleted), "skipped", len(result.Skipped))
	return result, nil
}

// parseSnapshotNames extracts snapshot names from the qm listsnapshot
// output. Lines look like ` `-> snapname  timestamp  desc`. We trim
// the leading box-drawing prefix and pick the first whitespace-
// separated token.
func parseSnapshotNames(qmOutput []byte) []string {
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(qmOutput))
	for sc.Scan() {
		raw := sc.Text()
		trimmed := strings.TrimLeft(raw, " `->|")
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if name == "current" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// timestampFromName extracts the unix-timestamp suffix from a
// SnapshotPrefix-shaped name. Returns ok=false if the name does not
// match the expected shape so the caller can skip it.
func timestampFromName(name string) (int64, bool) {
	tail, ok := strings.CutPrefix(name, SnapshotPrefix)
	if !ok || tail == "" {
		return 0, false
	}
	var n int64
	for _, r := range tail {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int64(r-'0')
	}
	return n, true
}
