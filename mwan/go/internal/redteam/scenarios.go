package redteam

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/ops"
)

// ---------------------------------------------------------------------------
// Preset: fault injection configuration
// ---------------------------------------------------------------------------

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

var Presets = map[string]Preset{
	"ipv4-loss": {
		Description: "IPv4 fails, IPv6 passes -> partial alert",
		HostV4Fail:  true,
	},
	"ipv6-loss": {
		Description: "IPv6 fails, IPv4 passes -> partial alert",
		HostV6Fail:  true,
	},
	"total-loss-mwan": {
		Description:       "Both fail, ISP up -> MWAN routing failure -> rollback",
		HostV4Fail:        true,
		HostV6Fail:        true,
		GuestDefaultFail:  true,
		GuestIfaceSucceed: true,
		DeployTSMode:      deployTSModeRecentThenStale,
		InjectSnapshot:    false,
	},
	"total-loss-isp": {
		Description:      "Both fail, no recent config change -> real outage, no rollback",
		HostV4Fail:       true,
		HostV6Fail:       true,
		GuestDefaultFail: true,
		GuestIfaceFail:   true,
		OmitDeployMarker: true,
	},
	"vm-crash": {
		Description: "VM appears stopped -> watchdog waits",
		VMStopped:   true,
	},
	"guest-agent-down": {
		Description:   "Guest agent fails -> diagnosis degraded",
		HostV4Fail:    true,
		HostV6Fail:    true,
		GuestExecFail: true,
	},
	"proxmox-routing": {
		Description:      "Host fails, VM has internet, no config change -> Proxmox-side issue",
		HostV4Fail:       true,
		HostV6Fail:       true,
		OmitDeployMarker: true,
	},
	"config-drift": {
		Description:         "No deploy marker; change marker + known-good snapshot -> rollback",
		HostV4Fail:          true,
		HostV6Fail:          true,
		GuestDefaultFail:    true,
		GuestIfaceSucceed:   true,
		OmitDeployMarker:    true,
		InjectChangeMarker:  true,
		InjectSnapshot:      true,
		InjectKnownGoodSnap: true,
	},
}

// ---------------------------------------------------------------------------
// Ops: wraps ops.SysOps and injects preset faults
// ---------------------------------------------------------------------------

type Ops struct {
	inner  ops.SysOps
	preset Preset
	log    *slog.Logger

	deployTSInjected bool
}

// NewOps creates a new red-team Ops wrapper around a SysOps implementation.
func NewOps(inner ops.SysOps, preset Preset, log *slog.Logger) *Ops {
	return &Ops{
		inner:  inner,
		preset: preset,
		log:    log,
	}
}

func (r *Ops) VMStatus(ctx context.Context, vmid string) (bool, error) {
	if r.preset.VMStopped {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "vm_stopped",
			"vmid", vmid,
		)
		return false, nil
	}
	return r.inner.VMStatus(ctx, vmid)
}

func (r *Ops) VMStop(ctx context.Context, vmid string) error {
	return r.inner.VMStop(ctx, vmid)
}

func (r *Ops) VMRollback(ctx context.Context, vmid, snap string) error {
	return r.inner.VMRollback(ctx, vmid, snap)
}

func (r *Ops) VMStart(ctx context.Context, vmid string) error {
	return r.inner.VMStart(ctx, vmid)
}

func (r *Ops) VMSnapshots(ctx context.Context, vmid string) ([]byte, error) {
	if r.preset.InjectSnapshot {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "fake_snapshot",
			"vmid", vmid,
		)
		var fake string
		if r.preset.InjectKnownGoodSnap {
			fake = fmt.Sprintf(
				"`-> known-good-%s\n",
				time.Now().Format("20060102-150405"),
			)
		} else {
			fake = fmt.Sprintf(
				"`-> pre-deploy-%s\n",
				time.Now().Format("20060102-150405"),
			)
		}
		return []byte(fake), nil
	}
	return r.inner.VMSnapshots(ctx, vmid)
}

func (r *Ops) VMSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	return r.inner.VMSnapshot(ctx, vmid, snapName)
}

func (r *Ops) VMDelSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	return r.inner.VMDelSnapshot(ctx, vmid, snapName)
}

func (r *Ops) GuestExec(
	ctx context.Context, vmid string, args ...string,
) (ops.GuestExecResult, error) {
	if r.preset.GuestExecFail {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "guest_exec_fail",
			"vmid", vmid,
			"args", strings.Join(args, " "),
		)
		return ops.GuestExecResult{ExitCode: 1}, fmt.Errorf("red-team: guest agent down")
	}
	isPing := len(args) > 0 && (args[0] == "ping" || args[0] == "ping6")
	hasIfaceFlag := false
	for _, a := range args {
		if a == "-I" {
			hasIfaceFlag = true
			break
		}
	}
	isCatDeploy := len(args) >= 2 &&
		args[0] == "cat" &&
		strings.Contains(args[1], "mwan-last-deploy")
	isCatChange := len(args) >= 2 &&
		args[0] == "cat" &&
		strings.Contains(args[1], "mwan-last-change")
	if isPing && hasIfaceFlag && r.preset.GuestIfaceFail {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "guest_iface_fail",
			"vmid", vmid,
			"args", strings.Join(args, " "),
		)
		return ops.GuestExecResult{ExitCode: 1}, nil
	}
	if isPing && hasIfaceFlag && r.preset.GuestIfaceSucceed {
		r.log.Info(
			"[RED TEAM] injecting success",
			"fault", "guest_iface_succeed",
			"vmid", vmid,
			"args", strings.Join(args, " "),
		)
		return ops.GuestExecResult{ExitCode: 0}, nil
	}
	if isPing && !hasIfaceFlag && r.preset.GuestDefaultFail {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "guest_default_route_fail",
			"vmid", vmid,
			"args", strings.Join(args, " "),
		)
		return ops.GuestExecResult{ExitCode: 1}, nil
	}
	if isCatDeploy && r.preset.OmitDeployMarker {
		return ops.GuestExecResult{ExitCode: 1}, nil
	}
	if isCatDeploy {
		if r.preset.DeployTSMode == deployTSModeRecentThenStale && r.deployTSInjected {
			oldTS := time.Now().Unix() - 7200
			r.log.Info(
				"[RED TEAM] suppressing repeat deploy marker",
				"fault", "inject_deploy_ts_once",
				"vmid", vmid,
				"deploy_ts", oldTS,
			)
			return ops.GuestExecResult{
				ExitCode: 0,
				Stdout:   strconv.FormatInt(oldTS, 10),
			}, nil
		}
		if r.preset.DeployTSMode == deployTSModeAlwaysRecent ||
			r.preset.DeployTSMode == deployTSModeRecentThenStale {
			ts := time.Now().Unix() - 60
			r.log.Info(
				"[RED TEAM] injecting fault",
				"fault", "inject_deploy_ts",
				"vmid", vmid,
				"deploy_ts", ts,
			)
			r.deployTSInjected = true
			return ops.GuestExecResult{
				ExitCode: 0,
				Stdout:   strconv.FormatInt(ts, 10),
			}, nil
		}
	}
	if isCatChange && r.preset.InjectChangeMarker {
		ts := time.Now().Unix() - 60
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "inject_change_ts",
			"vmid", vmid,
			"change_ts", ts,
		)
		return ops.GuestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(ts, 10),
		}, nil
	}
	return r.inner.GuestExec(ctx, vmid, args...)
}

func (r *Ops) Ping(ctx context.Context, bin, target string) bool {
	if bin == "ping" && r.preset.HostV4Fail {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "host_v4_fail",
			"target", target,
		)
		return false
	}
	if bin == "ping6" && r.preset.HostV6Fail {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "host_v6_fail",
			"target", target,
		)
		return false
	}
	return r.inner.Ping(ctx, bin, target)
}

func (r *Ops) SendEmail(ctx context.Context, to, subject, body string) error {
	return r.inner.SendEmail(ctx, to, subject, body)
}

func (r *Ops) GetConfigState(
	ctx context.Context, vmid string,
) (*mwanv1.GetConfigStateResponse, string, error) {
	return r.inner.GetConfigState(ctx, vmid)
}
