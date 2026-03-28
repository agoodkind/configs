package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// redTeamPreset: fault injection configuration
// ---------------------------------------------------------------------------

type redTeamPreset struct {
	Description       string
	HostV4Fail        bool
	HostV6Fail        bool
	VMStopped         bool
	GuestExecFail     bool
	GuestDefaultFail  bool
	GuestIfaceFail    bool
	GuestIfaceSucceed bool // force per-interface ISP pings to succeed (simulate ISP up)
	InjectDeployTS     bool
	InjectSnapshot     bool
	InjectChangeMarker  bool
	InjectKnownGoodSnap bool
	OmitDeployMarker    bool
}

var redTeamPresets = map[string]redTeamPreset{
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
		InjectDeployTS:    true,
		InjectSnapshot:    false,
	},
	"total-loss-isp": {
		Description:      "Both fail, ISP also down -> real outage, no rollback",
		HostV4Fail:       true,
		HostV6Fail:       true,
		GuestDefaultFail: true,
		GuestIfaceFail:   true,
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
		Description: "Host fails, VM has internet -> Proxmox-side issue",
		HostV4Fail:  true,
		HostV6Fail:  true,
	},
	"config-drift": {
		Description: "No deploy marker; change marker + known-good snapshot -> rollback",
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
// redTeamOps: wraps sysOps and injects preset faults
// ---------------------------------------------------------------------------

type redTeamOps struct {
	inner  sysOps
	preset redTeamPreset
	log    *slog.Logger
	// nc is passed through so guestExec can match against the configured paths.
	nc networkConfig
}

func (r *redTeamOps) vmStatus(ctx context.Context, vmid string) (bool, error) {
	if r.preset.VMStopped {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "vm_stopped",
			"vmid", vmid,
		)
		return false, nil
	}
	return r.inner.vmStatus(ctx, vmid)
}

func (r *redTeamOps) vmStop(ctx context.Context, vmid string) error {
	return r.inner.vmStop(ctx, vmid)
}

func (r *redTeamOps) vmRollback(ctx context.Context, vmid, snap string) error {
	return r.inner.vmRollback(ctx, vmid, snap)
}

func (r *redTeamOps) vmStart(ctx context.Context, vmid string) error {
	return r.inner.vmStart(ctx, vmid)
}

func (r *redTeamOps) vmSnapshots(ctx context.Context, vmid string) ([]byte, error) {
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
	return r.inner.vmSnapshots(ctx, vmid)
}

func (r *redTeamOps) vmSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	return r.inner.vmSnapshot(ctx, vmid, snapName)
}

func (r *redTeamOps) vmDelSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	return r.inner.vmDelSnapshot(ctx, vmid, snapName)
}

func (r *redTeamOps) guestExec(
	ctx context.Context, vmid string, args ...string,
) (guestExecResult, error) {
	if r.preset.GuestExecFail {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "guest_exec_fail",
			"vmid", vmid,
			"args", strings.Join(args, " "),
		)
		return guestExecResult{ExitCode: 1}, fmt.Errorf("red-team: guest agent down")
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
		return guestExecResult{ExitCode: 1}, nil
	}
	if isPing && hasIfaceFlag && r.preset.GuestIfaceSucceed {
		r.log.Info(
			"[RED TEAM] injecting success",
			"fault", "guest_iface_succeed",
			"vmid", vmid,
			"args", strings.Join(args, " "),
		)
		return guestExecResult{ExitCode: 0}, nil
	}
	if isPing && !hasIfaceFlag && r.preset.GuestDefaultFail {
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "guest_default_route_fail",
			"vmid", vmid,
			"args", strings.Join(args, " "),
		)
		return guestExecResult{ExitCode: 1}, nil
	}
	if isCatDeploy && r.preset.OmitDeployMarker {
		return guestExecResult{ExitCode: 1}, nil
	}
	if isCatDeploy && r.preset.InjectDeployTS {
		ts := time.Now().Unix() - 60
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "inject_deploy_ts",
			"vmid", vmid,
			"deploy_ts", ts,
		)
		return guestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(ts, 10),
		}, nil
	}
	if isCatChange && r.preset.InjectChangeMarker {
		ts := time.Now().Unix() - 60
		r.log.Info(
			"[RED TEAM] injecting fault",
			"fault", "inject_change_ts",
			"vmid", vmid,
			"change_ts", ts,
		)
		return guestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(ts, 10),
		}, nil
	}
	return r.inner.guestExec(ctx, vmid, args...)
}

func (r *redTeamOps) ping(ctx context.Context, bin, target string) bool {
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
	return r.inner.ping(ctx, bin, target)
}

func (r *redTeamOps) sendEmail(ctx context.Context, to, subject, body string) error {
	return r.inner.sendEmail(ctx, to, subject, body)
}
