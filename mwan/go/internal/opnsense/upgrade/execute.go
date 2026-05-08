package upgrade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
)

// Execute runs the in-guest upgrade per design section 4.2 and
// resolved decision 11.3. The execution channel is the QGA-shaped
// Executor interface. The mwan-opnsense RPC fallback is a stub today;
// resolved decision 11.3 documents that path so MWAN-153 or a sibling
// slice can wire it in without re-shaping the function signature.
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

	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		executingState.Phase = PhaseExecuteHung
		if saveErr := saveStateCtx(ctx, opts.StateDir, executingState, clk.Now()); saveErr != nil {
			slog.WarnContext(ctx, "upgrade.Execute: save hung state failed", "err", saveErr)
		}
		emit(ctx, deps.Notifier, slog.LevelError, KindExecute, opts.VMID,
			"opnsense-upgrade execute: hung after watchdog timeout",
			slog.Duration("timeout", timeout),
			slog.String("vmid", opts.VMID),
		)
		hungErr := fmt.Errorf("upgrade.Execute: hung after %s", timeout)
		slog.ErrorContext(ctx, "upgrade.Execute: hung", "err", hungErr, "vmid", opts.VMID, "timeout", timeout)
		return executingState, hungErr
	}
	if err != nil {
		executingState.Phase = PhaseExecuteFailed
		if saveErr := saveStateCtx(ctx, opts.StateDir, executingState, clk.Now()); saveErr != nil {
			slog.WarnContext(ctx, "upgrade.Execute: save failed state failed", "err", saveErr)
		}
		emit(ctx, deps.Notifier, slog.LevelError, KindExecute, opts.VMID,
			"opnsense-upgrade execute: guest exec returned error",
			slog.String("vmid", opts.VMID),
			slog.String("err", err.Error()),
		)
		slog.ErrorContext(ctx, "upgrade.Execute: GuestExec failed", "err", err, "vmid", opts.VMID)
		return executingState, fmt.Errorf("upgrade.Execute: GuestExec: %w", err)
	}
	if res.ExitCode != 0 {
		executingState.Phase = PhaseExecuteFailed
		if saveErr := saveStateCtx(ctx, opts.StateDir, executingState, clk.Now()); saveErr != nil {
			slog.WarnContext(ctx, "upgrade.Execute: save failed state failed", "err", saveErr)
		}
		emit(ctx, deps.Notifier, slog.LevelError, KindExecute, opts.VMID,
			"opnsense-upgrade execute: non-zero exit",
			slog.String("vmid", opts.VMID),
			slog.Int("exit_code", res.ExitCode),
		)
		exitErr := fmt.Errorf("upgrade.Execute: exit=%d", res.ExitCode)
		slog.ErrorContext(ctx, "upgrade.Execute: non-zero exit", "err", exitErr, "vmid", opts.VMID, "exit", res.ExitCode)
		return executingState, exitErr
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
	slog.InfoContext(ctx, "upgrade.Execute: clean exit", "vmid", opts.VMID, "dry_run", opts.DryRunExecute)
	return executingState, nil
}

// upgradeCommand builds the argv that the guest will run. With
// --dry-run-execute the command becomes `opnsense-upgrade -c` which is
// the documented check-only mode (resolved decision 11.4). The real
// path is `opnsense-upgrade -r <target>`.
func upgradeCommand(target string, dryRun bool) []string {
	if dryRun {
		return []string{"opnsense-upgrade", "-c"}
	}
	if target == "" {
		return []string{"opnsense-upgrade"}
	}
	return []string{"opnsense-upgrade", "-r", target}
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
