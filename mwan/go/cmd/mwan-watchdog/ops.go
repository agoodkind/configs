package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	mwanv1 "github.com/agoodkind/infra-tools/gen/mwan/v1"
	"github.com/agoodkind/infra-tools/pkg/pveapi"
	mailer "github.com/agoodkind/send-email/mailer"
	"github.com/mdlayher/vsock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	emailSender = "vault-watchdog@goodkind.io"

	defaultVsockCID  = 113
	defaultVsockPort = 50051

	timeoutQmStatus       = 10 * time.Second
	timeoutQmGuestExec    = 30 * time.Second
	timeoutQmStop         = 60 * time.Second
	timeoutQmRollback     = 120 * time.Second
	timeoutQmStart        = 60 * time.Second
	timeoutQmListSnapshot = 10 * time.Second
	timeoutQmSnapshot     = 120 * time.Second
	timeoutQmDelSnapshot  = 120 * time.Second
	timeoutHostProbe      = 20 * time.Second
	timeoutVsockRPC       = 15 * time.Second
	timeoutTCPRPC         = 15 * time.Second
	timeoutPVEExec        = 45 * time.Second
)

// ---------------------------------------------------------------------------
// sysOps: interface for all external dependencies
// ---------------------------------------------------------------------------

type guestExecResult struct {
	ExitCode int
	Stdout   string
}

type sysOps interface {
	vmStatus(ctx context.Context, vmid string) (bool, error)
	vmStop(ctx context.Context, vmid string) error
	vmRollback(ctx context.Context, vmid, snap string) error
	vmStart(ctx context.Context, vmid string) error
	vmSnapshots(ctx context.Context, vmid string) ([]byte, error)
	vmSnapshot(ctx context.Context, vmid, snapName string) error
	vmDelSnapshot(ctx context.Context, vmid, snapName string) error
	guestExec(
		ctx context.Context, vmid string, args ...string,
	) (guestExecResult, error)
	ping(ctx context.Context, bin, target string) bool
	sendEmail(
		ctx context.Context, to, subject, body string,
	) error
}

// ---------------------------------------------------------------------------
// realOps: gRPC-over-vsock primary, PVE REST fallback, qm lifecycle
// ---------------------------------------------------------------------------

type realOps struct {
	mailerCfg mailer.Config
	pve       *pveapi.Client
	vsockCID  uint32
	vsockPort uint32
	pveNode   string
	nc        networkConfig

	// testVsockOverride, if set, replaces vsockExec inside guestExec (unit tests only).
	testVsockOverride func(
		ctx context.Context, args ...string,
	) (guestExecResult, error)

	// testGrpcDialer, if set, replaces vsock.Dial in vsockExec (unit tests only).
	testGrpcDialer func(ctx context.Context, addr string) (net.Conn, error)

	tcpAddr string
	tracker *channelTracker

	// testTCPDialer, if set, replaces net.Dial in tcpExec (unit tests only).
	testTCPDialer func(ctx context.Context, addr string) (net.Conn, error)
}

func newRealOps(cfg config, nc networkConfig) *realOps {
	var pveClient *pveapi.Client
	if cfg.PVETokenID != "" && cfg.PVESecret != "" {
		pveClient = pveapi.NewClient(
			cfg.PVEBaseURL,
			cfg.PVETokenID,
			cfg.PVESecret,
		)
	}
	return &realOps{
		mailerCfg: mailer.Config{
			SMTP2GOAPIKey:     cfg.SMTP2GOAPIKey,
			DefaultFromDomain: "goodkind.io",
			Transport:         mailer.MethodAuto,
		},
		pve:       pveClient,
		vsockCID:  cfg.VsockCID,
		vsockPort: cfg.VsockPort,
		pveNode:   cfg.PVENode,
		nc:        nc,
		tcpAddr:   cfg.MwanAgentTCPAddr,
		tracker:   newChannelTracker(),
	}
}

func runCmd(
	ctx context.Context,
	timeout time.Duration,
	name string,
	args ...string,
) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return exec.CommandContext(cctx, name, args...).CombinedOutput()
}

func (r *realOps) vmStatus(ctx context.Context, vmid string) (bool, error) {
	out, err := runCmd(ctx, timeoutQmStatus, "qm", "status", vmid)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "running"), nil
}

func (r *realOps) vmStop(ctx context.Context, vmid string) error {
	_, err := runCmd(ctx, timeoutQmStop, "qm", "stop", vmid, "--timeout", "30")
	return err
}

func (r *realOps) vmRollback(ctx context.Context, vmid, snap string) error {
	_, err := runCmd(ctx, timeoutQmRollback, "qm", "rollback", vmid, snap)
	return err
}

func (r *realOps) vmStart(ctx context.Context, vmid string) error {
	_, err := runCmd(ctx, timeoutQmStart, "qm", "start", vmid)
	return err
}

func (r *realOps) vmSnapshots(ctx context.Context, vmid string) ([]byte, error) {
	return runCmd(ctx, timeoutQmListSnapshot, "qm", "listsnapshot", vmid)
}

func (r *realOps) vmSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	_, err := runCmd(ctx, timeoutQmSnapshot, "qm", "snapshot", vmid, snapName)
	return err
}

func (r *realOps) vmDelSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	_, err := runCmd(
		ctx, timeoutQmDelSnapshot, "qm", "delsnapshot", vmid, snapName,
	)
	return err
}

// guestExec tries all three channels in order: vsock -> TCP/mgmt -> PVE REST.
// Each channel's result is recorded in the channelTracker regardless of outcome.
func (r *realOps) guestExec(
	ctx context.Context, vmid string, args ...string,
) (guestExecResult, error) {
	// Allow unit test overrides to bypass the real transport layer.
	if r.testVsockOverride != nil {
		res, err := r.testVsockOverride(ctx, args...)
		if err == nil {
			return res, nil
		}
		return r.pveExec(ctx, vmid, args...)
	}

	// Channel 1: vsock
	vsockRes, vsockErr := r.vsockExec(ctx, args...)
	if vsockErr == nil {
		r.tracker.recordSuccess(chanVsock)
		return vsockRes, nil
	}
	r.tracker.recordFailure(chanVsock, vsockErr)

	// Channel 2: TCP management interface
	tcpRes, tcpErr := r.tcpExec(ctx, args...)
	if tcpErr == nil {
		r.tracker.recordSuccess(chanTCP)
		return tcpRes, nil
	}
	r.tracker.recordFailure(chanTCP, tcpErr)

	// Channel 3: PVE REST API fallback
	pveRes, pveErr := r.pveExec(ctx, vmid, args...)
	if pveErr == nil {
		r.tracker.recordSuccess(chanPVE)
	} else {
		r.tracker.recordFailure(chanPVE, pveErr)
	}
	return pveRes, pveErr
}

func (r *realOps) vsockExec(
	ctx context.Context, args ...string,
) (guestExecResult, error) {
	cctx, cancel := context.WithTimeout(ctx, timeoutVsockRPC)
	defer cancel()
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		return vsock.Dial(r.vsockCID, r.vsockPort, nil)
	}
	if r.testGrpcDialer != nil {
		dialer = r.testGrpcDialer
	}
	conn, err := grpc.NewClient(
		"passthrough:///mwan",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	if err != nil {
		return guestExecResult{ExitCode: 1}, err
	}
	defer func() { _ = conn.Close() }()

	cli := mwanv1.NewMWANAgentClient(conn)

	if len(args) == 0 {
		return guestExecResult{ExitCode: 1}, fmt.Errorf("vsockExec: no args")
	}
	switch args[0] {
	case "ping", "ping6":
		req := &mwanv1.PingRequest{
			Target:         pingTarget(args),
			BindInterface:  pingIface(args),
			Count:          pingCount(args, 2),
			TimeoutSeconds: 3,
		}
		resp, err := cli.Ping(cctx, req)
		if err != nil {
			return guestExecResult{ExitCode: 1}, err
		}
		if resp.GetSuccess() {
			return guestExecResult{ExitCode: 0}, nil
		}
		return guestExecResult{ExitCode: 1}, nil
	case "cat":
		if len(args) >= 2 && strings.Contains(args[1], "mwan-last-deploy") {
			resp, err := cli.GetConfigState(cctx, &mwanv1.GetConfigStateRequest{})
			if err != nil {
				return guestExecResult{ExitCode: 1}, err
			}
			ts := strconv.FormatInt(resp.GetLastDeployEpoch(), 10)
			return guestExecResult{ExitCode: 0, Stdout: ts}, nil
		}
	}
	return guestExecResult{ExitCode: 1},
		fmt.Errorf("vsockExec: unhandled command %q", args[0])
}

func (r *realOps) tcpExec(
	ctx context.Context, args ...string,
) (guestExecResult, error) {
	if r.tcpAddr == "" {
		return guestExecResult{ExitCode: 1}, fmt.Errorf("tcpExec: no tcp addr configured")
	}
	cctx, cancel := context.WithTimeout(ctx, timeoutTCPRPC)
	defer cancel()
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", r.tcpAddr)
	}
	if r.testTCPDialer != nil {
		dialer = r.testTCPDialer
	}
	conn, err := grpc.NewClient(
		"passthrough:///mwan-tcp",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	if err != nil {
		return guestExecResult{ExitCode: 1}, err
	}
	defer func() { _ = conn.Close() }()

	cli := mwanv1.NewMWANAgentClient(conn)

	if len(args) == 0 {
		return guestExecResult{ExitCode: 1}, fmt.Errorf("tcpExec: no args")
	}
	switch args[0] {
	case "ping", "ping6":
		req := &mwanv1.PingRequest{
			Target:         pingTarget(args),
			BindInterface:  pingIface(args),
			Count:          pingCount(args, 2),
			TimeoutSeconds: 3,
		}
		resp, err := cli.Ping(cctx, req)
		if err != nil {
			return guestExecResult{ExitCode: 1}, err
		}
		if resp.GetSuccess() {
			return guestExecResult{ExitCode: 0}, nil
		}
		return guestExecResult{ExitCode: 1}, nil
	case "cat":
		if len(args) >= 2 && strings.Contains(args[1], "mwan-last-deploy") {
			resp, err := cli.GetConfigState(cctx, &mwanv1.GetConfigStateRequest{})
			if err != nil {
				return guestExecResult{ExitCode: 1}, err
			}
			ts := strconv.FormatInt(resp.GetLastDeployEpoch(), 10)
			return guestExecResult{ExitCode: 0, Stdout: ts}, nil
		}
	}
	return guestExecResult{ExitCode: 1},
		fmt.Errorf("tcpExec: unhandled command %q", args[0])
}

func (r *realOps) pveExec(
	ctx context.Context, vmid string, args ...string,
) (guestExecResult, error) {
	if r.pve == nil {
		return guestExecResult{ExitCode: 1},
			fmt.Errorf("pve client not configured (no PVE_TOKEN_ID)")
	}
	cctx, cancel := context.WithTimeout(ctx, timeoutPVEExec)
	defer cancel()
	pid, err := r.pve.GuestExec(cctx, r.pveNode, vmid, args)
	if err != nil {
		return guestExecResult{ExitCode: 1}, err
	}
	code, stdout, _, err := r.pve.GuestExecStatus(cctx, r.pveNode, vmid, pid)
	if err != nil {
		return guestExecResult{ExitCode: 1}, err
	}
	return guestExecResult{ExitCode: code, Stdout: stdout}, nil
}

func (r *realOps) ping(ctx context.Context, bin, target string) bool {
	cctx, cancel := context.WithTimeout(ctx, timeoutHostProbe)
	defer cancel()
	return exec.CommandContext(cctx, bin, "-c", "2", "-W", "3", target).Run() == nil
}

func (r *realOps) sendEmail(ctx context.Context, to, subject, body string) error {
	m := mailer.New(r.mailerCfg)
	return m.Send(ctx, mailer.Message{
		To:      to,
		From:    emailSender,
		Subject: subject,
		Body:    body,
		Caller:  "mwan-watchdog",
	})
}

// ---------------------------------------------------------------------------
// ping arg helpers (parse argv-style ping arguments for vsock translation)
// ---------------------------------------------------------------------------

func pingTarget(args []string) string {
	for i, a := range args {
		if a == "-I" || a == "-c" || a == "-W" {
			i++
			_ = i
			continue
		}
		if !strings.HasPrefix(a, "-") && i > 0 {
			return a
		}
	}
	if len(args) > 0 {
		return args[len(args)-1]
	}
	return ""
}

func pingIface(args []string) string {
	for i, a := range args {
		if a == "-I" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func pingCount(args []string, def int32) int32 {
	for i, a := range args {
		if a == "-c" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err == nil {
				return int32(n)
			}
		}
	}
	return def
}

// ---------------------------------------------------------------------------
// dryRunOps: wraps sysOps, logs destructive operations instead of executing
// ---------------------------------------------------------------------------

type dryRunOps struct {
	inner sysOps
	log   *slog.Logger
}

func (d *dryRunOps) vmStatus(ctx context.Context, vmid string) (bool, error) {
	return d.inner.vmStatus(ctx, vmid)
}

func (d *dryRunOps) vmStop(_ context.Context, vmid string) error {
	d.log.Info("[DRY-RUN] would stop VM", "vmid", vmid)
	return nil
}

func (d *dryRunOps) vmRollback(_ context.Context, vmid, snap string) error {
	d.log.Info("[DRY-RUN] would rollback VM", "vmid", vmid, "snapshot", snap)
	return nil
}

func (d *dryRunOps) vmStart(_ context.Context, vmid string) error {
	d.log.Info("[DRY-RUN] would start VM", "vmid", vmid)
	return nil
}

func (d *dryRunOps) vmSnapshots(ctx context.Context, vmid string) ([]byte, error) {
	return d.inner.vmSnapshots(ctx, vmid)
}

func (d *dryRunOps) vmSnapshot(_ context.Context, vmid, snapName string) error {
	d.log.Info(
		"[DRY-RUN] would snapshot VM",
		"vmid", vmid,
		"snapshot", snapName,
	)
	return nil
}

func (d *dryRunOps) vmDelSnapshot(_ context.Context, vmid, snapName string) error {
	d.log.Info(
		"[DRY-RUN] would delete snapshot",
		"vmid", vmid,
		"snapshot", snapName,
	)
	return nil
}

func (d *dryRunOps) guestExec(
	ctx context.Context, vmid string, args ...string,
) (guestExecResult, error) {
	return d.inner.guestExec(ctx, vmid, args...)
}

func (d *dryRunOps) ping(ctx context.Context, bin, target string) bool {
	return d.inner.ping(ctx, bin, target)
}

func (d *dryRunOps) sendEmail(_ context.Context, to, subject, _ string) error {
	d.log.Info(
		"[DRY-RUN] would send email",
		"to", to,
		"subject", subject,
	)
	return nil
}
