// Package redteam injects controlled faults into watchdog dependencies.
package redteam

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/ops"
)

// ---------------------------------------------------------------------------
// Preset: fault injection configuration
// ---------------------------------------------------------------------------

// Preset describes one bundle of injected host, guest, and snapshot faults.
type Preset struct {
	Description         string
	HostV4Fail          bool
	HostV6Fail          bool
	VMStopped           bool
	GuestExecFail       bool
	GuestDefaultFail    bool
	GuestIfaceFail      bool
	GuestIfaceSucceed   bool // force per-interface ISP pings to succeed (simulate ISP up)
	DeployTSMode        deployTSMode
	InjectSnapshot      bool
	InjectChangeMarker  bool
	InjectKnownGoodSnap bool
	OmitDeployMarker    bool
}

type deployTSMode string

const (
	deployTSModeNone            deployTSMode = "none"
	deployTSModeAlwaysRecent    deployTSMode = "always_recent"
	deployTSModeRecentThenStale deployTSMode = "recent_then_stale"
)

// Presets is the named catalog of supported fault-injection scenarios.
var Presets = map[string]Preset{
	"ipv4-loss": {
		Description:         "IPv4 fails, IPv6 passes -> partial alert",
		HostV4Fail:          true,
		HostV6Fail:          false,
		VMStopped:           false,
		GuestExecFail:       false,
		GuestDefaultFail:    false,
		GuestIfaceFail:      false,
		GuestIfaceSucceed:   false,
		DeployTSMode:        deployTSModeNone,
		InjectSnapshot:      false,
		InjectChangeMarker:  false,
		InjectKnownGoodSnap: false,
		OmitDeployMarker:    false,
	},
	"ipv6-loss": {
		Description:         "IPv6 fails, IPv4 passes -> partial alert",
		HostV4Fail:          false,
		HostV6Fail:          true,
		VMStopped:           false,
		GuestExecFail:       false,
		GuestDefaultFail:    false,
		GuestIfaceFail:      false,
		GuestIfaceSucceed:   false,
		DeployTSMode:        deployTSModeNone,
		InjectSnapshot:      false,
		InjectChangeMarker:  false,
		InjectKnownGoodSnap: false,
		OmitDeployMarker:    false,
	},
	"total-loss-mwan": {
		Description:         "Both fail, ISP up -> MWAN routing failure -> rollback",
		HostV4Fail:          true,
		HostV6Fail:          true,
		VMStopped:           false,
		GuestExecFail:       false,
		GuestDefaultFail:    true,
		GuestIfaceFail:      false,
		GuestIfaceSucceed:   true,
		DeployTSMode:        deployTSModeRecentThenStale,
		InjectSnapshot:      false,
		InjectChangeMarker:  false,
		InjectKnownGoodSnap: false,
		OmitDeployMarker:    false,
	},
	"total-loss-isp": {
		Description:         "Both fail, no recent config change -> real outage, no rollback",
		HostV4Fail:          true,
		HostV6Fail:          true,
		VMStopped:           false,
		GuestExecFail:       false,
		GuestDefaultFail:    true,
		GuestIfaceFail:      true,
		GuestIfaceSucceed:   false,
		DeployTSMode:        deployTSModeNone,
		InjectSnapshot:      false,
		InjectChangeMarker:  false,
		InjectKnownGoodSnap: false,
		OmitDeployMarker:    true,
	},
	"vm-crash": {
		Description:         "VM appears stopped -> watchdog waits",
		HostV4Fail:          false,
		HostV6Fail:          false,
		VMStopped:           true,
		GuestExecFail:       false,
		GuestDefaultFail:    false,
		GuestIfaceFail:      false,
		GuestIfaceSucceed:   false,
		DeployTSMode:        deployTSModeNone,
		InjectSnapshot:      false,
		InjectChangeMarker:  false,
		InjectKnownGoodSnap: false,
		OmitDeployMarker:    false,
	},
	"guest-agent-down": {
		Description:         "Guest agent fails -> diagnosis degraded",
		HostV4Fail:          true,
		HostV6Fail:          true,
		VMStopped:           false,
		GuestExecFail:       true,
		GuestDefaultFail:    false,
		GuestIfaceFail:      false,
		GuestIfaceSucceed:   false,
		DeployTSMode:        deployTSModeNone,
		InjectSnapshot:      false,
		InjectChangeMarker:  false,
		InjectKnownGoodSnap: false,
		OmitDeployMarker:    false,
	},
	"proxmox-routing": {
		Description:         "Host fails, VM has internet, no config change -> Proxmox-side issue",
		HostV4Fail:          true,
		HostV6Fail:          true,
		VMStopped:           false,
		GuestExecFail:       false,
		GuestDefaultFail:    false,
		GuestIfaceFail:      false,
		GuestIfaceSucceed:   false,
		DeployTSMode:        deployTSModeNone,
		InjectSnapshot:      false,
		InjectChangeMarker:  false,
		InjectKnownGoodSnap: false,
		OmitDeployMarker:    true,
	},
	"config-drift": {
		Description:         "No deploy marker; change marker + known-good snapshot -> rollback",
		HostV4Fail:          true,
		HostV6Fail:          true,
		VMStopped:           false,
		GuestExecFail:       false,
		GuestDefaultFail:    true,
		GuestIfaceFail:      false,
		GuestIfaceSucceed:   true,
		DeployTSMode:        deployTSModeNone,
		OmitDeployMarker:    true,
		InjectChangeMarker:  true,
		InjectSnapshot:      true,
		InjectKnownGoodSnap: true,
	},
}

// ---------------------------------------------------------------------------
// Ops: wraps ops.SysOps and injects preset faults
// ---------------------------------------------------------------------------

// Ops wraps SysOps and injects the selected preset's failures.
type Ops struct {
	inner  ops.SysOps
	preset Preset
	log    *slog.Logger
	now    func() time.Time

	deployTSInjected bool
}

// NewOps creates a new red-team Ops wrapper around a SysOps implementation.
func NewOps(inner ops.SysOps, preset Preset, log *slog.Logger) *Ops {
	return NewOpsWithClock(inner, preset, log, time.Now)
}

// NewOpsWithClock builds a red-team wrapper with an injected test clock.
func NewOpsWithClock(
	inner ops.SysOps,
	preset Preset,
	log *slog.Logger,
	now func() time.Time,
) *Ops {
	if now == nil {
		now = time.Now
	}
	return &Ops{
		inner:            inner,
		preset:           preset,
		log:              log,
		now:              now,
		deployTSInjected: false,
	}
}

// VMStatus injects a stopped-VM result when the preset requests it.
func (r *Ops) VMStatus(ctx context.Context, vmid string) (bool, error) {
	if r.preset.VMStopped {
		r.log.InfoContext(
			ctx,
			"[RED TEAM] injecting fault",
			"fault", "vm_stopped",
			"vmid", vmid,
		)
		return false, nil
	}
	running, err := r.inner.VMStatus(ctx, vmid)
	if err != nil {
		r.log.ErrorContext(ctx, "red-team vm status failed", "vmid", vmid, "err", err)
		return false, fmt.Errorf("red-team vm status: %w", err)
	}
	return running, nil
}

// VMStop delegates to the wrapped SysOps implementation.
func (r *Ops) VMStop(ctx context.Context, vmid string) error {
	if err := r.inner.VMStop(ctx, vmid); err != nil {
		r.log.ErrorContext(ctx, "red-team vm stop failed", "vmid", vmid, "err", err)
		return fmt.Errorf("red-team vm stop: %w", err)
	}
	return nil
}

// VMRollback delegates to the wrapped SysOps implementation.
func (r *Ops) VMRollback(ctx context.Context, vmid, snap string) error {
	if err := r.inner.VMRollback(ctx, vmid, snap); err != nil {
		r.log.ErrorContext(ctx, "red-team vm rollback failed", "vmid", vmid, "snapshot", snap, "err", err)
		return fmt.Errorf("red-team vm rollback: %w", err)
	}
	return nil
}

// VMStart delegates to the wrapped SysOps implementation.
func (r *Ops) VMStart(ctx context.Context, vmid string) error {
	if err := r.inner.VMStart(ctx, vmid); err != nil {
		r.log.ErrorContext(ctx, "red-team vm start failed", "vmid", vmid, "err", err)
		return fmt.Errorf("red-team vm start: %w", err)
	}
	return nil
}

// VMSnapshots injects a synthetic rollback snapshot when requested.
func (r *Ops) VMSnapshots(ctx context.Context, vmid string) ([]byte, error) {
	if r.preset.InjectSnapshot {
		r.log.InfoContext(
			ctx,
			"[RED TEAM] injecting fault",
			"fault", "fake_snapshot",
			"vmid", vmid,
		)
		suffix := r.now().Format("20060102-150405")
		var fake string
		if r.preset.InjectKnownGoodSnap {
			fake = fmt.Sprintf("`-> known-good-%s\n", suffix)
		} else {
			fake = fmt.Sprintf("`-> pre-deploy-%s\n", suffix)
		}
		return []byte(fake), nil
	}
	output, err := r.inner.VMSnapshots(ctx, vmid)
	if err != nil {
		r.log.ErrorContext(ctx, "red-team vm snapshots failed", "vmid", vmid, "err", err)
		return nil, fmt.Errorf("red-team vm snapshots: %w", err)
	}
	return output, nil
}

// VMSnapshot delegates to the wrapped SysOps implementation.
func (r *Ops) VMSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	if err := r.inner.VMSnapshot(ctx, vmid, snapName); err != nil {
		r.log.ErrorContext(ctx, "red-team vm snapshot failed", "vmid", vmid, "snapshot", snapName, "err", err)
		return fmt.Errorf("red-team vm snapshot: %w", err)
	}
	return nil
}

// VMDelSnapshot delegates to the wrapped SysOps implementation.
func (r *Ops) VMDelSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	if err := r.inner.VMDelSnapshot(ctx, vmid, snapName); err != nil {
		r.log.ErrorContext(ctx, "red-team vm delete snapshot failed", "vmid", vmid, "snapshot", snapName, "err", err)
		return fmt.Errorf("red-team vm delete snapshot: %w", err)
	}
	return nil
}

// GuestExec injects preset guest failures before falling back to inner.
func (r *Ops) GuestExec(
	ctx context.Context, vmid string, args ...string,
) (ops.GuestExecResult, error) {
	if r.preset.GuestExecFail {
		r.logFault(ctx, "guest_exec_fail", vmid, args)
		return ops.GuestExecResult{ExitCode: 1, Stdout: ""},
			fmt.Errorf("red-team: guest agent down")
	}
	if res, handled := r.handlePingFault(ctx, vmid, args); handled {
		return res, nil
	}
	if res, handled := r.handleDeployFault(ctx, vmid, args); handled {
		return res, nil
	}
	if res, handled := r.handleChangeFault(ctx, vmid, args); handled {
		return res, nil
	}
	result, err := r.inner.GuestExec(ctx, vmid, args...)
	if err != nil {
		r.log.ErrorContext(ctx, "red-team guest exec failed", "vmid", vmid, "err", err)
		return result, fmt.Errorf("red-team guest exec: %w", err)
	}
	return result, nil
}

func (r *Ops) logFault(ctx context.Context, fault, vmid string, args []string) {
	r.log.InfoContext(
		ctx,
		"[RED TEAM] injecting fault",
		"fault", fault,
		"vmid", vmid,
		"args", strings.Join(args, " "),
	)
}

func classifyGuestArgs(args []string) (isPing, hasIface, isCatDeploy bool) {
	isPing = len(args) > 0 && (args[0] == "ping" || args[0] == "ping6")
	if slices.Contains(args, "-I") {
		hasIface = true
	}
	isCatDeploy = len(args) >= 2 && args[0] == "cat" && strings.Contains(args[1], "last-deploy")
	return
}

func (r *Ops) handlePingFault(
	ctx context.Context,
	vmid string,
	args []string,
) (ops.GuestExecResult, bool) {
	isPing, hasIface, _ := classifyGuestArgs(args)
	if isPing && hasIface && r.preset.GuestIfaceFail {
		r.logFault(ctx, "guest_iface_fail", vmid, args)
		return ops.GuestExecResult{ExitCode: 1, Stdout: ""}, true
	}
	if isPing && hasIface && r.preset.GuestIfaceSucceed {
		r.logFault(ctx, "guest_iface_succeed", vmid, args)
		return ops.GuestExecResult{ExitCode: 0, Stdout: ""}, true
	}
	if isPing && !hasIface && r.preset.GuestDefaultFail {
		r.logFault(ctx, "guest_default_route_fail", vmid, args)
		return ops.GuestExecResult{ExitCode: 1, Stdout: ""}, true
	}
	return ops.GuestExecResult{ExitCode: 0, Stdout: ""}, false
}

func (r *Ops) handleDeployFault(
	ctx context.Context,
	vmid string,
	args []string,
) (ops.GuestExecResult, bool) {
	_, _, isCatDeploy := classifyGuestArgs(args)
	if !isCatDeploy {
		return ops.GuestExecResult{ExitCode: 0, Stdout: ""}, false
	}
	if r.preset.OmitDeployMarker {
		return ops.GuestExecResult{ExitCode: 1, Stdout: ""}, true
	}
	if r.preset.DeployTSMode == deployTSModeRecentThenStale && r.deployTSInjected {
		oldTS := r.now().Unix() - 7200
		r.logFault(ctx, "inject_deploy_ts_once", vmid, args)
		return ops.GuestExecResult{ExitCode: 0, Stdout: strconv.FormatInt(oldTS, 10)}, true
	}
	if r.preset.DeployTSMode == deployTSModeAlwaysRecent ||
		r.preset.DeployTSMode == deployTSModeRecentThenStale {
		ts := r.now().Unix() - 60
		r.logFault(ctx, "inject_deploy_ts", vmid, args)
		r.deployTSInjected = true
		return ops.GuestExecResult{ExitCode: 0, Stdout: strconv.FormatInt(ts, 10)}, true
	}
	return ops.GuestExecResult{ExitCode: 0, Stdout: ""}, false
}

func (r *Ops) handleChangeFault(
	ctx context.Context,
	vmid string,
	args []string,
) (ops.GuestExecResult, bool) {
	isCatChange := len(args) >= 2 &&
		args[0] == "cat" &&
		strings.Contains(args[1], "mwan-last-change")
	if !isCatChange || !r.preset.InjectChangeMarker {
		return ops.GuestExecResult{ExitCode: 0, Stdout: ""}, false
	}
	ts := r.now().Unix() - 60
	r.logFault(ctx, "inject_change_ts", vmid, args)
	return ops.GuestExecResult{ExitCode: 0, Stdout: strconv.FormatInt(ts, 10)}, true
}

// Ping injects host-side loss for the matching address family when requested.
func (r *Ops) Ping(ctx context.Context, bin, target string) bool {
	if bin == "ping" && r.preset.HostV4Fail {
		r.log.InfoContext(
			ctx,
			"[RED TEAM] injecting fault",
			"fault", "host_v4_fail",
			"target", target,
		)
		return false
	}
	if bin == "ping6" && r.preset.HostV6Fail {
		r.log.InfoContext(
			ctx,
			"[RED TEAM] injecting fault",
			"fault", "host_v6_fail",
			"target", target,
		)
		return false
	}
	return r.inner.Ping(ctx, bin, target)
}

// GetConfigState delegates to the wrapped SysOps implementation.
func (r *Ops) GetConfigState(
	ctx context.Context, vmid string,
) (*mwanv1.GetConfigStateResponse, string, error) {
	state, channel, err := r.inner.GetConfigState(ctx, vmid)
	if err != nil {
		r.log.ErrorContext(ctx, "red-team get config state failed", "vmid", vmid, "err", err)
		return nil, "", fmt.Errorf("red-team get config state: %w", err)
	}
	return state, channel, nil
}

// GetBGPStatus delegates to the wrapped SysOps implementation.
func (r *Ops) GetBGPStatus(
	ctx context.Context, vmid string,
) (*mwanv1.GetBGPStatusResponse, error) {
	state, err := r.inner.GetBGPStatus(ctx, vmid)
	if err != nil {
		r.log.ErrorContext(ctx, "red-team get bgp status failed", "vmid", vmid, "err", err)
		return nil, fmt.Errorf("red-team get bgp status: %w", err)
	}
	return state, nil
}

// AnnounceRoutes delegates to the wrapped SysOps implementation.
func (r *Ops) AnnounceRoutes(ctx context.Context, vmid string) error {
	if err := r.inner.AnnounceRoutes(ctx, vmid); err != nil {
		r.log.ErrorContext(ctx, "red-team announce routes failed", "vmid", vmid, "err", err)
		return fmt.Errorf("red-team announce routes: %w", err)
	}
	return nil
}

// WithdrawRoutes delegates to the wrapped SysOps implementation.
func (r *Ops) WithdrawRoutes(ctx context.Context, vmid string) error {
	if err := r.inner.WithdrawRoutes(ctx, vmid); err != nil {
		r.log.ErrorContext(ctx, "red-team withdraw routes failed", "vmid", vmid, "err", err)
		return fmt.Errorf("red-team withdraw routes: %w", err)
	}
	return nil
}
