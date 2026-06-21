package watchdog

import (
	"context"
	"fmt"
	"log/slog"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/ops"
)

// dryRunOps wraps an ops.SysOps and logs destructive operations without executing them.
type dryRunOps struct {
	inner ops.SysOps
	log   *slog.Logger
}

func (d *dryRunOps) VMStatus(ctx context.Context, vmid string) (bool, error) {
	running, err := d.inner.VMStatus(ctx, vmid)
	if err != nil {
		d.log.ErrorContext(ctx, "watchdog dry run vm status failed", "vmid", vmid, "err", err)
		return false, fmt.Errorf("watchdog dry run vm status: %w", err)
	}
	return running, nil
}

func (d *dryRunOps) VMStop(ctx context.Context, vmid string) error {
	d.log.InfoContext(ctx, "[DRY-RUN] would stop VM", "vmid", vmid)
	return nil
}

func (d *dryRunOps) VMRollback(ctx context.Context, vmid, snap string) error {
	d.log.InfoContext(ctx, "[DRY-RUN] would rollback VM", "vmid", vmid, "snapshot", snap)
	return nil
}

func (d *dryRunOps) VMStart(ctx context.Context, vmid string) error {
	d.log.InfoContext(ctx, "[DRY-RUN] would start VM", "vmid", vmid)
	return nil
}

func (d *dryRunOps) VMSnapshots(ctx context.Context, vmid string) ([]byte, error) {
	output, err := d.inner.VMSnapshots(ctx, vmid)
	if err != nil {
		d.log.ErrorContext(ctx, "watchdog dry run vm snapshots failed", "vmid", vmid, "err", err)
		return nil, fmt.Errorf("watchdog dry run vm snapshots: %w", err)
	}
	return output, nil
}

func (d *dryRunOps) VMSnapshot(ctx context.Context, vmid, snapName string) error {
	d.log.InfoContext(
		ctx,
		"[DRY-RUN] would snapshot VM",
		"vmid", vmid,
		"snapshot", snapName,
	)
	return nil
}

func (d *dryRunOps) VMDelSnapshot(ctx context.Context, vmid, snapName string) error {
	d.log.InfoContext(
		ctx,
		"[DRY-RUN] would delete snapshot",
		"vmid", vmid,
		"snapshot", snapName,
	)
	return nil
}

func (d *dryRunOps) GuestExec(
	ctx context.Context, vmid string, args ...string,
) (ops.GuestExecResult, error) {
	result, err := d.inner.GuestExec(ctx, vmid, args...)
	if err != nil {
		d.log.ErrorContext(ctx, "watchdog dry run guest exec failed", "vmid", vmid, "err", err)
		return result, fmt.Errorf("watchdog dry run guest exec: %w", err)
	}
	return result, nil
}

func (d *dryRunOps) Ping(ctx context.Context, bin, target string) bool {
	return d.inner.Ping(ctx, bin, target)
}

func (d *dryRunOps) GetConfigState(
	ctx context.Context, vmid string,
) (*mwanv1.GetConfigStateResponse, string, error) {
	state, channel, err := d.inner.GetConfigState(ctx, vmid)
	if err != nil {
		d.log.ErrorContext(ctx, "watchdog dry run get config state failed", "vmid", vmid, "err", err)
		return nil, "", fmt.Errorf("watchdog dry run get config state: %w", err)
	}
	return state, channel, nil
}

func (d *dryRunOps) GetBGPStatus(
	ctx context.Context, vmid string,
) (*mwanv1.GetBGPStatusResponse, error) {
	d.log.InfoContext(ctx, "[DRY-RUN] would get BGP status", "vmid", vmid)
	return &mwanv1.GetBGPStatusResponse{}, nil
}

func (d *dryRunOps) AnnounceRoutes(ctx context.Context, vmid string) error {
	d.log.InfoContext(ctx, "[DRY-RUN] would announce BGP routes", "vmid", vmid)
	return nil
}

func (d *dryRunOps) WithdrawRoutes(ctx context.Context, vmid string) error {
	d.log.InfoContext(ctx, "[DRY-RUN] would withdraw BGP routes", "vmid", vmid)
	return nil
}
