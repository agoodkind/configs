package watchdog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"goodkind.io/mwan/internal/ops"
	"goodkind.io/mwan/internal/rollback"
)

func (w *watchdog) findSnapshot(ctx context.Context) (string, error) {
	log := w.tracedLogger(ctx)
	log.InfoContext(ctx, "Listing snapshots for VM", "vmid", w.cfg.MwanVMID)
	out, err := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID)
	if err != nil {
		log.ErrorContext(ctx, "find snapshot failed", "vmid", w.cfg.MwanVMID, "err", err)
		return "", fmt.Errorf("find snapshot: %w", err)
	}
	snap := rollback.ExtractLatestSnapshot(out)
	if snap == "" {
		log.InfoContext(ctx,
			"No rollback snapshot (pre-deploy-* or known-good-*)",
			"listsnapshot_output", string(out),
		)
	} else {
		log.InfoContext(ctx, "Found rollback snapshot", "snapshot", snap)
	}
	return snap, nil
}

func (w *watchdog) readGuestUnix(ctx context.Context, path string) (int64, bool) {
	log := w.tracedLogger(ctx)
	parsed, err := w.ops.GuestExec(ctx, w.cfg.MwanVMID, "cat", path)
	if err != nil {
		if errors.Is(err, ops.ErrGuestExecUnavailable) {
			log.WarnContext(ctx,
				"PVE guest-exec unavailable; cannot read deploy timestamp; assuming no recent deploy",
				"vmid", w.cfg.MwanVMID,
			)
		} else {
			log.ErrorContext(ctx, "guestExec(cat) error", "path", path, "err", err)
		}
		return 0, false
	}
	if parsed.ExitCode != 0 {
		return 0, false
	}
	raw := strings.TrimSpace(parsed.Stdout)
	if raw == "" || raw == "null" {
		return 0, false
	}
	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		log.ErrorContext(ctx,
			"guest timestamp parse error",
			"path", path,
			"raw", raw,
			"err", err,
		)
		return 0, false
	}
	return ts, true
}

// parseManifest parses a manifest in sha256sum(1) format: "<hash>  <path>\n".
// Returns a map of path -> sha256hex. Lines that don't match the format are
// silently skipped.
func parseManifest(raw string) map[string]string {
	m := make(map[string]string)
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// sha256sum format: 64 hex chars, two spaces, then the path.
		if len(line) < 66 || line[64] != ' ' || line[65] != ' ' {
			continue
		}
		hash := line[:64]
		path := line[66:]
		if path != "" {
			m[path] = hash
		}
	}
	return m
}

// categorizeManifestChanges compares two path->sha256hex maps and returns
// sorted lists of changed, added, and removed paths.
func categorizeManifestChanges(prev, curr map[string]string) (changed, added, removed []string) {
	for path, hash := range curr {
		if oldHash, ok := prev[path]; !ok {
			added = append(added, path)
		} else if hash != oldHash {
			changed = append(changed, path)
		}
	}
	for path := range prev {
		if _, ok := curr[path]; !ok {
			removed = append(removed, path)
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(removed)
	return changed, added, removed
}

// manifestDiff compares two path->sha256hex maps and returns a formatted
// summary of changed, added, and removed files for inclusion in an email.
func manifestDiff(prev, curr map[string]string) string {
	if len(curr) == 0 {
		return "  (manifest unavailable for current state)\n"
	}
	if len(prev) == 0 {
		var lines []string
		for path := range curr {
			lines = append(lines, "  "+path)
		}
		sort.Strings(lines)
		return strings.Join(lines, "\n") + "\n"
	}

	changed, added, removed := categorizeManifestChanges(prev, curr)
	if len(changed) == 0 && len(added) == 0 && len(removed) == 0 {
		return "  (no per-file diff available; composite hash changed)\n"
	}
	var sb strings.Builder
	for _, p := range changed {
		sb.WriteString("  modified: ")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	for _, p := range added {
		sb.WriteString("  added:    ")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	for _, p := range removed {
		sb.WriteString("  removed:  ")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (w *watchdog) checkConfigHash(ctx context.Context) {
	log := w.tracedLogger(ctx)
	resp, usedChannel, err := w.ops.GetConfigState(ctx, w.cfg.MwanVMID)
	if err != nil {
		log.WarnContext(ctx, "checkConfigHash getConfigState", "err", err)
		w.lastHashCheckOK = false
		return
	}
	w.routeChannelFallback(ctx, usedChannel)
	h := strings.TrimSpace(resp.GetConfigHash())
	if h == "" {
		return
	}
	currentManifest := parseManifest(resp.GetConfigManifest())

	if w.lastConfigHash != "" && h != w.lastConfigHash {
		if !w.postRollbackGraceUntil.IsZero() &&
			w.now().Before(w.postRollbackGraceUntil) {
			log.InfoContext(ctx,
				"Post-rollback hash change suppressed",
				"old_hash", w.lastConfigHash,
				"new_hash", h,
				"grace_until", w.postRollbackGraceUntil,
			)
		} else {
			w.hashChangeWindowStart = w.now().Unix()
			diffSection := manifestDiff(w.lastManifest, currentManifest)
			log.WarnContext(ctx,
				"config hash drift detected",
				"old_hash", w.lastConfigHash,
				"new_hash", resp.GetConfigHash(),
				"changed_files", diffSection,
				"vm_id", w.cfg.MwanVMID,
				"node", w.cfg.PVE.Node,
				"change_window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
			)
		}
	} else {
		log.DebugContext(ctx,
			"config hash check: no drift",
			"hash", h,
			"channel", usedChannel,
		)
		w.lastHashCheckOK = true
	}
	w.lastConfigHash = h
	w.lastManifest = currentManifest
}

func (w *watchdog) maybeSnapshot(ctx context.Context) {
	log := w.tracedLogger(ctx)
	if w.cfg.Watchdog.SnapshotHealthyThreshold <= 0 {
		return
	}
	if w.consecutiveHealthy < w.cfg.Watchdog.SnapshotHealthyThreshold {
		return
	}
	windowSec := int64(w.cfg.Watchdog.DeployWindowMinutes) * 60
	if w.hashChangeWindowStart > 0 {
		elapsed := w.now().Unix() - w.hashChangeWindowStart
		if elapsed < windowSec {
			return
		}
	}
	deployTS, dOK := w.readGuestUnix(ctx, w.cfg.Network.LastDeployPath)
	if dOK && (w.now().Unix()-deployTS) < windowSec {
		return
	}
	minGap := time.Duration(w.cfg.Watchdog.MinSnapshotIntervalSeconds) * time.Second
	if !w.lastSnapshotAt.IsZero() && w.since(w.lastSnapshotAt) < minGap {
		return
	}
	if w.cfg.Watchdog.HashCheckEveryNHealthy > 0 && !w.lastHashCheckOK {
		return
	}
	name := "known-good-" + w.now().Format("20060102-150405")
	if err := w.ops.VMSnapshot(ctx, w.cfg.MwanVMID, name); err != nil {
		log.ErrorContext(ctx, "vmSnapshot failed", "err", err, "snapshot", name)
		return
	}
	log.InfoContext(ctx, "created known-good snapshot", "snapshot", name)
	w.lastSnapshotAt = w.now()
	w.consecutiveHealthy = 0
	if err := w.pruneSnapshots(ctx); err != nil {
		log.ErrorContext(ctx, "pruneSnapshots failed", "err", err)
	}
	log.InfoContext(ctx, "Interface monitor stopped (context cancelled)")
}

func (w *watchdog) pruneSnapshots(ctx context.Context) error {
	log := w.tracedLogger(ctx)
	out, err := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID)
	if err != nil {
		return fmt.Errorf("prune snapshots list: %w", err)
	}
	s := string(out)
	knownGoods := rollback.KnownGoodSnapRE.FindAllString(s, -1)
	sort.Strings(knownGoods)
	preDeploys := rollback.PreDeploySnapRE.FindAllString(s, -1)
	total := len(knownGoods) + len(preDeploys)

	if w.cfg.Watchdog.MaxKnownGoodSnapshots > 0 &&
		len(knownGoods) > w.cfg.Watchdog.MaxKnownGoodSnapshots {
		toDrop := len(knownGoods) - w.cfg.Watchdog.MaxKnownGoodSnapshots
		for i := range toDrop {
			if err := w.ops.VMDelSnapshot(
				ctx, w.cfg.MwanVMID, knownGoods[i],
			); err != nil {
				log.ErrorContext(ctx,
					"vmDelSnapshot",
					"snapshot", knownGoods[i],
					"err", err,
				)
				return fmt.Errorf("prune snapshot %q: %w", knownGoods[i], err)
			}
		}
		out, err = w.ops.VMSnapshots(ctx, w.cfg.MwanVMID)
		if err != nil {
			return fmt.Errorf("prune snapshots refresh: %w", err)
		}
		s = string(out)
		knownGoods = rollback.KnownGoodSnapRE.FindAllString(s, -1)
		sort.Strings(knownGoods)
		preDeploys = rollback.PreDeploySnapRE.FindAllString(s, -1)
		total = len(knownGoods) + len(preDeploys)
	}

	if w.cfg.Watchdog.MaxTotalSnapshots <= 0 ||
		total <= w.cfg.Watchdog.MaxTotalSnapshots || len(knownGoods) == 0 {
		return nil
	}
	excess := min(total-w.cfg.Watchdog.MaxTotalSnapshots, len(knownGoods))
	for i := range excess {
		if err := w.ops.VMDelSnapshot(
			ctx, w.cfg.MwanVMID, knownGoods[i],
		); err != nil {
			log.ErrorContext(ctx,
				"vmDelSnapshot max total",
				"snapshot", knownGoods[i],
				"err", err,
			)
			return fmt.Errorf("prune snapshot %q for max total: %w", knownGoods[i], err)
		}
	}
	return nil
}

func (w *watchdog) checkDeploy(ctx context.Context) (int64, bool) {
	log := w.tracedLogger(ctx)
	running, err := w.ops.VMStatus(ctx, w.cfg.MwanVMID)
	if err != nil {
		log.ErrorContext(ctx, "checkDeploy: vmStatus error", "err", err)
		return 0, false
	}
	if !running {
		log.InfoContext(ctx,
			"checkDeploy: VM is not running; cannot check change window",
			"vmid", w.cfg.MwanVMID,
		)
		return 0, false
	}

	log.InfoContext(ctx,
		"checkDeploy: reading change window markers",
		"last_deploy_path", w.cfg.Network.LastDeployPath,
		"last_change_path", w.cfg.Network.LastChangePath,
		"vmid", w.cfg.MwanVMID,
	)

	deployTS, dOK := w.readGuestUnix(ctx, w.cfg.Network.LastDeployPath)
	changeTS, cOK := w.readGuestUnix(ctx, w.cfg.Network.LastChangePath)

	var candidates []int64
	if dOK {
		candidates = append(candidates, deployTS)
	}
	if cOK {
		candidates = append(candidates, changeTS)
	}
	if w.hashChangeWindowStart > 0 {
		candidates = append(candidates, w.hashChangeWindowStart)
	}
	if len(candidates) == 0 {
		log.InfoContext(ctx, "checkDeploy: no change markers or hash window")
		return 0, false
	}
	effective := candidates[0]
	for _, t := range candidates[1:] {
		if t > effective {
			effective = t
		}
	}

	ageMin := (w.now().Unix() - effective) / 60
	log.InfoContext(ctx,
		"checkDeploy: change window",
		"deploy_ts", deployTS,
		"deploy_ok", dOK,
		"change_ts", changeTS,
		"change_ok", cOK,
		"hash_window_ts", w.hashChangeWindowStart,
		"effective_ts", effective,
		"age_minutes", ageMin,
		"window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
	)
	if ageMin > int64(w.cfg.Watchdog.DeployWindowMinutes) {
		log.InfoContext(ctx,
			"checkDeploy: change window stale",
			"age_minutes", ageMin,
			"window_minutes", w.cfg.Watchdog.DeployWindowMinutes,
		)
		return 0, false
	}

	log.InfoContext(ctx,
		"checkDeploy: within change window",
		"effective_ts", effective,
		"age_minutes", ageMin,
	)
	return effective, true
}

// executeRollbackVM performs the stop-rollback-start cycle on the MWAN VM.
// It deletes intermediate snapshots that are children of the target (Proxmox/ZFS
// requires the target to be a leaf), then runs qm rollback and qm start.
// Returns a non-nil error if the qm rollback command itself failed.
func (w *watchdog) executeRollbackVM(ctx context.Context, snap string) error {
	log := w.tracedLogger(ctx)
	stopStart := w.now()
	log.InfoContext(ctx,
		"Stopping VM",
		"vmid", w.cfg.MwanVMID,
		"timeout", ops.TimeoutQmStop,
	)
	if err := w.ops.VMStop(ctx, w.cfg.MwanVMID); err != nil {
		log.ErrorContext(ctx,
			"vmStop error (continuing to rollback)",
			"vmid", w.cfg.MwanVMID,
			"err", err,
		)
	} else {
		log.DebugContext(ctx,
			"VM stopped",
			"vmid", w.cfg.MwanVMID,
			"elapsed", w.since(stopStart).Round(time.Millisecond),
		)
	}

	// Delete any watchdog-managed snapshots that are children of the target.
	// Proxmox/ZFS only allows rollback to the leaf snapshot in the chain.
	if listOut, lErr := w.ops.VMSnapshots(ctx, w.cfg.MwanVMID); lErr == nil {
		toDelete := rollback.SnapshotsAfter(listOut, snap)
		for _, child := range slices.Backward(toDelete) {
			log.DebugContext(ctx, "Deleting intermediate snapshot before rollback",
				"snapshot", child, "target", snap)
			if dErr := w.ops.VMDelSnapshot(ctx, w.cfg.MwanVMID, child); dErr != nil {
				log.ErrorContext(ctx, "Failed to delete intermediate snapshot",
					"snapshot", child, "err", dErr)
			}
		}
	} else {
		log.WarnContext(ctx, "Could not list snapshots before rollback", "err", lErr)
	}

	var rollbackErr error
	rollbackStart := w.now()
	log.InfoContext(ctx,
		"Running qm rollback",
		"vmid", w.cfg.MwanVMID,
		"snapshot", snap,
		"timeout", ops.TimeoutQmRollback,
	)
	if err := w.ops.VMRollback(ctx, w.cfg.MwanVMID, snap); err != nil {
		rollbackErr = err
		log.ErrorContext(ctx,
			"qm rollback FAILED; attempting qm start anyway",
			"vmid", w.cfg.MwanVMID,
			"snapshot", snap,
			"elapsed", w.since(rollbackStart).Round(time.Millisecond),
			"err", err,
		)
	} else {
		log.InfoContext(ctx,
			"qm rollback completed",
			"elapsed", w.since(rollbackStart).Round(time.Millisecond),
		)
	}

	startTime := w.now()
	log.InfoContext(ctx,
		"Starting VM",
		"vmid", w.cfg.MwanVMID,
		"timeout", ops.TimeoutQmStart,
	)
	if err := w.ops.VMStart(ctx, w.cfg.MwanVMID); err != nil {
		log.ErrorContext(ctx,
			"qm start FAILED; VM may remain stopped",
			"vmid", w.cfg.MwanVMID,
			"elapsed", w.since(startTime).Round(time.Millisecond),
			"err", err,
		)
	} else {
		log.InfoContext(ctx,
			"VM started",
			"vmid", w.cfg.MwanVMID,
			"elapsed", w.since(startTime).Round(time.Millisecond),
		)
	}
	return rollbackErr
}

// recordRollbackResult persists rollback state, removes the lock file, and
// logs the outcome. Called after executeRollbackVM completes.
func (w *watchdog) recordRollbackResult(
	ctx context.Context,
	deployTS int64,
	snap string,
	rollbackErr error,
) {
	log := w.tracedLogger(ctx)
	if err := os.Remove(w.cfg.Watchdog.RollbackLockFile); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		log.ErrorContext(ctx, "remove rollback lock", "err", err)
	} else {
		log.InfoContext(ctx, "Removed rollback lock file")
	}

	rollbackAttempts := 1
	if existing, att, _ := rollback.AlreadyDone(
		w.cfg.Watchdog.RollbackStateFile, deployTS,
	); !existing {
		rollbackAttempts = att + 1
	}
	rollbackSucceeded := rollbackErr == nil
	if writeErr := rollback.WriteState(
		w.cfg.Watchdog.RollbackStateFile, deployTS, snap,
		rollbackAttempts, rollbackSucceeded, w.now(),
	); writeErr != nil {
		log.ErrorContext(ctx, "write rollback state", "err", writeErr)
	} else {
		log.InfoContext(ctx,
			"Wrote rollback state",
			"path", w.cfg.Watchdog.RollbackStateFile,
			"deploy_ts", deployTS,
			"snapshot", snap,
			"success", rollbackSucceeded,
			"attempts", rollbackAttempts,
		)
	}

	log.WarnContext(ctx,
		"auto-rollback completed",
		"vm_id", w.cfg.MwanVMID,
		"snapshot", snap,
		"node", w.cfg.PVE.Node,
	)
}
