package upgrade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Execute runs the in-guest upgrade. The execution channel is the QGA-shaped
// Executor interface.
func Execute(ctx context.Context, deps Deps, opts Options) (State, error) {
	if err := validateOptions(opts); err != nil {
		slog.ErrorContext(ctx, "upgrade.Execute: invalid options", "err", err)
		return emptyState(), err
	}
	if deps.Exec == nil {
		err := errors.New("upgrade.Execute: deps.Exec is required")
		slog.ErrorContext(ctx, "upgrade.Execute: deps.Exec missing", "err", err)
		return emptyState(), err
	}
	clk := clockOrDefault(deps.Clock)
	now := clk.Now()

	cur, err := loadStateCtx(ctx, opts.StateDir, opts.VMID)
	if err != nil {
		return emptyState(), err
	}
	if err := EnforceTransition(cur.Phase, PhaseExecuting); err != nil {
		slog.ErrorContext(ctx, "upgrade.Execute: refusing transition",
			"err", err, "from", cur.Phase, "to", PhaseExecuting)
		return cur, err
	}

	timeout := opts.UpgradeTimeout
	if timeout <= 0 {
		timeout = DefaultUpgradeTimeout
	}

	executingState := cur
	executingState.Phase = PhaseExecuting
	if err := saveStateCtx(ctx, opts.StateDir, executingState, now); err != nil {
		return emptyState(), err
	}

	args := upgradeCommand(opts.Target, opts.DryRunExecute)
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := deps.Exec.GuestExec(execCtx, opts.VMID, args...)

	deployDir := deployPathFor(opts.StateDir, opts.VMID, cur.DeployID)
	logBytes := fmt.Appendf(nil, "argv=%v\nexit=%d\nerr=%v\nstdout:\n%s\nstderr:\n%s\n",
		args, res.ExitCode, err, res.Stdout, res.Stderr)
	if writeErr := WriteFileBytes(ctx, filepath.Join(deployDir, "upgrade.log"), logBytes); writeErr != nil {
		slog.WarnContext(ctx, "upgrade.Execute: write upgrade.log failed", "err", writeErr)
	}

	failedState, returnErr := executeCheckExecResult(ctx, deps, opts, clk, execCtx, executingState, res, err, timeout)
	if returnErr != nil {
		return failedState, returnErr
	}

	preVersion, postVersion, rebootErr := rebootAndVerifyVersion(ctx, deps, opts.VMID, deployDir)
	if rebootErr != nil {
		executingState.Phase = PhaseExecuteFailed
		if saveErr := saveStateCtx(ctx, opts.StateDir, executingState, clk.Now()); saveErr != nil {
			slog.WarnContext(ctx, "upgrade.Execute: save failed state failed", "err", saveErr)
		}
		emit(ctx, deps.Notifier, slog.LevelError, KindExecute, opts.VMID,
			"opnsense-upgrade execute: post-reboot verification failed",
			slog.String("vmid", opts.VMID),
			slog.String("err", rebootErr.Error()),
		)
		return executingState, rebootErr
	}

	executingState.Phase = PhaseExecuted
	if err := saveStateCtx(ctx, opts.StateDir, executingState, clk.Now()); err != nil {
		return emptyState(), err
	}
	emit(ctx, deps.Notifier, slog.LevelInfo, KindExecute, opts.VMID,
		"opnsense-upgrade execute: upgrade command exited cleanly",
		slog.String("vmid", opts.VMID),
		slog.Bool("dry_run", opts.DryRunExecute),
	)
	slog.InfoContext(ctx, "upgrade.Execute: clean exit", "vmid", opts.VMID,
		"dry_run", opts.DryRunExecute,
		"pre_version", preVersion,
		"post_version", postVersion,
	)
	return executingState, nil
}

// executeCheckExecResult handles the hung/error/non-zero branches from
// GuestExec so Execute stays within the funlen limit. Returns a non-nil
// returnErr when the caller should propagate a failure.
func executeCheckExecResult(
	ctx context.Context,
	deps Deps,
	opts Options,
	clk Clock,
	execCtx context.Context,
	st State,
	res GuestExecResult,
	err error,
	timeout time.Duration,
) (State, error) {
	saveWarn := func(s State) {
		if saveErr := saveStateCtx(ctx, opts.StateDir, s, clk.Now()); saveErr != nil {
			slog.WarnContext(ctx, "upgrade.Execute: save failed state failed", "err", saveErr)
		}
	}
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		st.Phase = PhaseExecuteHung
		saveWarn(st)
		emit(ctx, deps.Notifier, slog.LevelError, KindExecute, opts.VMID,
			"opnsense-upgrade execute: hung after watchdog timeout",
			slog.Duration("timeout", timeout),
			slog.String("vmid", opts.VMID),
		)
		hungErr := fmt.Errorf("upgrade.Execute: hung after %s", timeout)
		slog.ErrorContext(ctx, "upgrade.Execute: hung", "err", hungErr, "vmid", opts.VMID, "timeout", timeout)
		return st, hungErr
	}
	if err != nil {
		st.Phase = PhaseExecuteFailed
		saveWarn(st)
		emit(ctx, deps.Notifier, slog.LevelError, KindExecute, opts.VMID,
			"opnsense-upgrade execute: guest exec returned error",
			slog.String("vmid", opts.VMID),
			slog.String("err", err.Error()),
		)
		slog.ErrorContext(ctx, "upgrade.Execute: GuestExec failed", "err", err, "vmid", opts.VMID)
		return st, fmt.Errorf("upgrade.Execute: GuestExec: %w", err)
	}
	if res.ExitCode != 0 {
		st.Phase = PhaseExecuteFailed
		saveWarn(st)
		emit(ctx, deps.Notifier, slog.LevelError, KindExecute, opts.VMID,
			"opnsense-upgrade execute: non-zero exit",
			slog.String("vmid", opts.VMID),
			slog.Int("exit_code", res.ExitCode),
		)
		exitErr := fmt.Errorf("upgrade.Execute: exit=%d", res.ExitCode)
		slog.ErrorContext(ctx, "upgrade.Execute: non-zero exit", "err", exitErr, "vmid", opts.VMID, "exit", res.ExitCode)
		return st, exitErr
	}
	return st, nil
}

// rebootAndVerifyVersion issues a guest reboot, waits for the guest to
// come back, then asserts the major version changed. It returns the pre-
// and post-upgrade major versions so the caller can log them. A non-nil
// error means the caller must transition to PhaseExecuteFailed.
func rebootAndVerifyVersion(ctx context.Context, deps Deps, vmid, deployDir string) (preVersion, postVersion string, err error) {
	// opnsense-update -u only stages packages; the install applies at
	// next boot. Issue a reboot now. The connection error that follows is
	// expected because the guest closes its end of the channel on shutdown.
	_, _ = deps.Exec.GuestExec(ctx, vmid, "shutdown", "-r", "+0")
	slog.InfoContext(ctx, "upgrade.Execute: reboot issued, waiting for guest", "vmid", vmid)

	if waitErr := waitForGuest(ctx, deps, vmid, DefaultPostRebootTimeout); waitErr != nil {
		slog.ErrorContext(ctx, "upgrade.Execute: waitForGuest failed after reboot", "err", waitErr, "vmid", vmid)
		return "", "", fmt.Errorf("upgrade.Execute: post-reboot waitForGuest: %w", waitErr)
	}

	postVersion, err = guestMajorVersion(ctx, deps, vmid)
	if err != nil {
		slog.ErrorContext(ctx, "upgrade.Execute: post-reboot version failed", "err", err, "vmid", vmid)
		return "", "", fmt.Errorf("upgrade.Execute: post-reboot version: %w", err)
	}

	preVersion, err = readPreVersion(deployDir)
	if err != nil {
		slog.ErrorContext(ctx, "upgrade.Execute: read pre-upgrade version failed", "err", err)
		return "", "", fmt.Errorf("upgrade.Execute: read pre-upgrade version: %w", err)
	}

	if postVersion == preVersion {
		versionErr := fmt.Errorf("upgrade.Execute: major version unchanged after reboot (pre=%s, post=%s)", preVersion, postVersion)
		slog.ErrorContext(ctx, "upgrade.Execute: version unchanged", "err", versionErr, "vmid", vmid)
		return preVersion, postVersion, versionErr
	}
	return preVersion, postVersion, nil
}

// guestMajorVersion runs opnsense-version inside the guest and returns
// the major component of the version string. opnsense-version prints a
// line like "OPNsense 25.7.11_9 (amd64)"; the major version is the
// integer before the first dot in the second whitespace-separated token.
func guestMajorVersion(ctx context.Context, deps Deps, vmid string) (string, error) {
	res, err := deps.Exec.GuestExec(ctx, vmid, "opnsense-version")
	if err != nil {
		slog.ErrorContext(ctx, "guestMajorVersion: GuestExec failed", "err", err, "vmid", vmid)
		return "", fmt.Errorf("guestMajorVersion: GuestExec: %w", err)
	}
	if res.ExitCode != 0 {
		exitErr := fmt.Errorf("guestMajorVersion: exit=%d stderr=%s", res.ExitCode, res.Stderr)
		slog.ErrorContext(ctx, "guestMajorVersion: non-zero exit", "err", exitErr, "vmid", vmid, "exit", res.ExitCode)
		return "", exitErr
	}
	return parseMajorVersion(res.Stdout)
}

// parseMajorVersion extracts the major version integer from a line of
// opnsense-version output. Input looks like "OPNsense 25.7.11_9 (amd64)"
// or "OPNsense 26.1 (amd64)". The function returns the string before the
// first dot in the version token (e.g. "25" or "26").
func parseMajorVersion(raw string) (string, error) {
	line := strings.TrimSpace(raw)
	// Discard everything after the first newline so a multi-line response
	// does not confuse the parse.
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	// Expected shape: "OPNsense <version> (arch)" where <version> is
	// "<major>.<minor>[._patch]".
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", fmt.Errorf("parseMajorVersion: unexpected output %q", raw)
	}
	version := parts[1]
	dotIdx := strings.IndexByte(version, '.')
	if dotIdx <= 0 {
		return "", fmt.Errorf("parseMajorVersion: no dot in version token %q", version)
	}
	return version[:dotIdx], nil
}

// readPreVersion reads version.txt from the deploy directory and returns
// the major version component. A zero-byte placeholder (written when the
// guest did not respond to opnsense-version during prepare) returns an
// empty string without error so the caller can decide how to handle it.
func readPreVersion(deployDir string) (string, error) {
	path := filepath.Clean(filepath.Join(deployDir, ArtefactVersion))
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Error("readPreVersion: read file failed", "err", err, "path", path)
		return "", fmt.Errorf("readPreVersion: %w", err)
	}
	if len(data) == 0 {
		return "", nil
	}
	return parseMajorVersion(string(data))
}

// upgradeCommand builds the argv that the guest will run. With
// --dry-run-execute the command becomes `opnsense-update -c` which is
// the check-only mode (resolved decision 11.4). The real path is
// `opnsense-update -u` (latest release) or `opnsense-update -u -r
// <target>` (specific release). `opnsense-upgrade` does not exist on
// OPNsense 25.7; the canonical major-release upgrade tool is
// opnsense-update. Source: opnsense/update src/update/opnsense-update.sh.in,
// getopts string line 293; -u case (DO_UPGRADE) and -r case (DO_RELEASE)
// at https://github.com/opnsense/update/blob/master/src/update/opnsense-update.sh.in.
func upgradeCommand(target string, dryRun bool) []string {
	if dryRun {
		return []string{"opnsense-update", "-c"}
	}
	if target == "" {
		return []string{"opnsense-update", "-u"}
	}
	return []string{"opnsense-update", "-u", "-r", target}
}

// waitForGuest is a small helper used by rollback to poll for QGA
// liveness. It polls every 2 seconds up to deadline.
func waitForGuest(ctx context.Context, deps Deps, vmid string, deadline time.Duration) error {
	if deps.Exec == nil {
		err := errors.New("waitForGuest: deps.Exec is required")
		slog.ErrorContext(ctx, "upgrade.waitForGuest: deps.Exec missing", "err", err)
		return err
	}
	pollCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	for {
		res, err := deps.Exec.GuestExec(pollCtx, vmid, "true")
		if err == nil && res.ExitCode == 0 {
			return nil
		}
		select {
		case <-pollCtx.Done():
			timedErr := fmt.Errorf("waitForGuest: timed out after %s", deadline)
			slog.WarnContext(ctx, "upgrade.waitForGuest: timed out",
				"err", timedErr, "vmid", vmid, "deadline", deadline)
			return timedErr
		case <-time.After(2 * time.Second):
		}
	}
}
