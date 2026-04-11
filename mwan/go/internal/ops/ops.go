package ops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mdlayher/vsock"
	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
	"goodkind.io/mwan/pkg/pveapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	TimeoutQmStatus       = 10 * time.Second
	timeoutQmGuestExec    = 30 * time.Second
	TimeoutQmStop         = 60 * time.Second
	TimeoutQmRollback     = 120 * time.Second
	TimeoutQmStart        = 60 * time.Second
	timeoutQmListSnapshot = 10 * time.Second
	TimeoutQmSnapshot     = 120 * time.Second
	timeoutQmDelSnapshot  = 120 * time.Second
	timeoutHostProbe      = 20 * time.Second
	timeoutVsockRPC       = 15 * time.Second
	timeoutTCPRPC         = 15 * time.Second
	timeoutPVEExec        = 45 * time.Second
)

// ErrGuestExecUnavailable is returned by pveExec when the PVE client is
// not configured (missing token). Callers can distinguish this from a
// command that ran and returned a non-zero exit code.
var ErrGuestExecUnavailable = errors.New("pve client not configured (no PVE_TOKEN_ID)")

// ---------------------------------------------------------------------------
// SysOps: interface for all external dependencies
// ---------------------------------------------------------------------------

type GuestExecResult struct {
	ExitCode int
	Stdout   string
}

type SysOps interface {
	VMStatus(ctx context.Context, vmid string) (bool, error)
	VMStop(ctx context.Context, vmid string) error
	VMRollback(ctx context.Context, vmid, snap string) error
	VMStart(ctx context.Context, vmid string) error
	VMSnapshots(ctx context.Context, vmid string) ([]byte, error)
	VMSnapshot(ctx context.Context, vmid, snapName string) error
	VMDelSnapshot(ctx context.Context, vmid, snapName string) error
	GuestExec(
		ctx context.Context, vmid string, args ...string,
	) (GuestExecResult, error)
	Ping(ctx context.Context, bin, target string) bool
	SendEmail(
		ctx context.Context, to, subject, body string,
	) error
	GetConfigState(
		ctx context.Context, vmid string,
	) (*mwanv1.GetConfigStateResponse, string, error)
	GetBGPStatus(
		ctx context.Context, vmid string,
	) (*mwanv1.GetBGPStatusResponse, error)
	AnnounceRoutes(ctx context.Context, vmid string) error
	WithdrawRoutes(ctx context.Context, vmid string) error
}

// ---------------------------------------------------------------------------
// RealOps: gRPC-over-vsock primary, PVE REST fallback, qm lifecycle
// ---------------------------------------------------------------------------

type RealOps struct {
	email     *email.Sender
	pve       *pveapi.Client
	vsockCID  uint32
	vsockPort uint32
	pveNode   string
	nc        config.NetworkConfig

	// testVsockOverride, if set, replaces vsockExec inside GuestExec (unit tests only).
	testVsockOverride func(
		ctx context.Context, args ...string,
	) (GuestExecResult, error)

	// testGrpcDialer, if set, replaces vsock.Dial in vsockExec (unit tests only).
	testGrpcDialer func(ctx context.Context, addr string) (net.Conn, error)

	tcpAddr string
	tracker *ChannelTracker

	// testTCPDialer, if set, replaces net.Dial in tcpExec (unit tests only).
	testTCPDialer func(ctx context.Context, addr string) (net.Conn, error)
}

func NewRealOps(cfg *config.Config, emailSender *email.Sender) *RealOps {
	var pveClient *pveapi.Client
	if cfg.PVE.TokenID != "" && cfg.PVE.TokenSecret != "" {
		pveClient = pveapi.NewClient(
			cfg.PVE.BaseURL,
			cfg.PVE.TokenID,
			cfg.PVE.TokenSecret,
		)
	}
	return &RealOps{
		email:     emailSender,
		pve:       pveClient,
		vsockCID:  cfg.Watchdog.VsockCID,
		vsockPort: cfg.Watchdog.VsockPort,
		pveNode:   cfg.PVE.Node,
		nc:        cfg.Network,
		tcpAddr:   cfg.Watchdog.MwanAgentTCPAddr,
		tracker:   NewChannelTracker(),
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

func (r *RealOps) VMStatus(ctx context.Context, vmid string) (bool, error) {
	out, err := runCmd(ctx, TimeoutQmStatus, "qm", "status", vmid)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "running"), nil
}

func (r *RealOps) VMStop(ctx context.Context, vmid string) error {
	_, err := runCmd(ctx, TimeoutQmStop, "qm", "stop", vmid, "--timeout", "30")
	return err
}

func (r *RealOps) VMRollback(ctx context.Context, vmid, snap string) error {
	_, err := runCmd(ctx, TimeoutQmRollback, "qm", "rollback", vmid, snap)
	return err
}

func (r *RealOps) VMStart(ctx context.Context, vmid string) error {
	_, err := runCmd(ctx, TimeoutQmStart, "qm", "start", vmid)
	return err
}

func (r *RealOps) VMSnapshots(ctx context.Context, vmid string) ([]byte, error) {
	return runCmd(ctx, timeoutQmListSnapshot, "qm", "listsnapshot", vmid)
}

func (r *RealOps) VMSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	_, err := runCmd(ctx, TimeoutQmSnapshot, "qm", "snapshot", vmid, snapName)
	return err
}

func (r *RealOps) VMDelSnapshot(
	ctx context.Context, vmid, snapName string,
) error {
	_, err := runCmd(
		ctx, timeoutQmDelSnapshot, "qm", "delsnapshot", vmid, snapName,
	)
	return err
}

// GuestExec tries all three channels in order: vsock -> TCP/mgmt -> PVE REST.
// Each channel's result is recorded in the channelTracker regardless of outcome.
func (r *RealOps) GuestExec(
	ctx context.Context, vmid string, args ...string,
) (GuestExecResult, error) {
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
		r.tracker.recordSuccess(ChanVsock)
		return vsockRes, nil
	}
	r.tracker.recordFailure(ChanVsock, vsockErr)

	// Channel 2: TCP management interface
	tcpRes, tcpErr := r.tcpExec(ctx, args...)
	if tcpErr == nil {
		r.tracker.recordSuccess(ChanTCP)
		return tcpRes, nil
	}
	r.tracker.recordFailure(ChanTCP, tcpErr)

	// Channel 3: PVE REST API fallback
	pveRes, pveErr := r.pveExec(ctx, vmid, args...)
	if pveErr == nil {
		r.tracker.recordSuccess(ChanPVE)
	} else {
		r.tracker.recordFailure(ChanPVE, pveErr)
	}
	return pveRes, pveErr
}

func (r *RealOps) vsockExec(
	ctx context.Context, args ...string,
) (GuestExecResult, error) {
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
		return GuestExecResult{ExitCode: 1}, err
	}
	defer func() { _ = conn.Close() }()

	cli := mwanv1.NewMWANAgentClient(conn)

	if len(args) == 0 {
		return GuestExecResult{ExitCode: 1}, fmt.Errorf("vsockExec: no args")
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
			return GuestExecResult{ExitCode: 1}, err
		}
		if resp.GetSuccess() {
			return GuestExecResult{ExitCode: 0}, nil
		}
		return GuestExecResult{ExitCode: 1}, nil
	case "cat":
		if len(args) >= 2 && strings.Contains(args[1], "mwan-last-deploy") {
			resp, err := cli.GetConfigState(cctx, &mwanv1.GetConfigStateRequest{})
			if err != nil {
				return GuestExecResult{ExitCode: 1}, err
			}
			ts := strconv.FormatInt(resp.GetLastDeployEpoch(), 10)
			return GuestExecResult{ExitCode: 0, Stdout: ts}, nil
		}
	}
	return GuestExecResult{ExitCode: 1},
		fmt.Errorf("vsockExec: unhandled command %q", args[0])
}

func (r *RealOps) tcpExec(
	ctx context.Context, args ...string,
) (GuestExecResult, error) {
	if r.tcpAddr == "" {
		return GuestExecResult{ExitCode: 1}, fmt.Errorf("tcpExec: no tcp addr configured")
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
		return GuestExecResult{ExitCode: 1}, err
	}
	defer func() { _ = conn.Close() }()

	cli := mwanv1.NewMWANAgentClient(conn)

	if len(args) == 0 {
		return GuestExecResult{ExitCode: 1}, fmt.Errorf("tcpExec: no args")
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
			return GuestExecResult{ExitCode: 1}, err
		}
		if resp.GetSuccess() {
			return GuestExecResult{ExitCode: 0}, nil
		}
		return GuestExecResult{ExitCode: 1}, nil
	case "cat":
		if len(args) >= 2 && strings.Contains(args[1], "mwan-last-deploy") {
			resp, err := cli.GetConfigState(cctx, &mwanv1.GetConfigStateRequest{})
			if err != nil {
				return GuestExecResult{ExitCode: 1}, err
			}
			ts := strconv.FormatInt(resp.GetLastDeployEpoch(), 10)
			return GuestExecResult{ExitCode: 0, Stdout: ts}, nil
		}
	}
	return GuestExecResult{ExitCode: 1},
		fmt.Errorf("tcpExec: unhandled command %q", args[0])
}

func (r *RealOps) pveExec(
	ctx context.Context, vmid string, args ...string,
) (GuestExecResult, error) {
	if r.pve == nil {
		return GuestExecResult{ExitCode: 1}, ErrGuestExecUnavailable
	}
	cctx, cancel := context.WithTimeout(ctx, timeoutPVEExec)
	defer cancel()
	pid, err := r.pve.GuestExec(cctx, r.pveNode, vmid, args)
	if err != nil {
		return GuestExecResult{ExitCode: 1}, err
	}
	code, stdout, _, err := r.pve.GuestExecStatus(cctx, r.pveNode, vmid, pid)
	if err != nil {
		return GuestExecResult{ExitCode: 1}, err
	}
	return GuestExecResult{ExitCode: code, Stdout: stdout}, nil
}

func (r *RealOps) vsockGetConfigState(
	ctx context.Context,
) (*mwanv1.GetConfigStateResponse, error) {
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
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cli := mwanv1.NewMWANAgentClient(conn)
	return cli.GetConfigState(cctx, &mwanv1.GetConfigStateRequest{})
}

func (r *RealOps) tcpGetConfigState(
	ctx context.Context,
) (*mwanv1.GetConfigStateResponse, error) {
	if r.tcpAddr == "" {
		return nil, fmt.Errorf("tcpGetConfigState: no tcp addr configured")
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
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cli := mwanv1.NewMWANAgentClient(conn)
	return cli.GetConfigState(cctx, &mwanv1.GetConfigStateRequest{})
}

func (r *RealOps) GetConfigState(
	ctx context.Context, vmid string,
) (*mwanv1.GetConfigStateResponse, string, error) {
	_ = vmid
	res, err := r.vsockGetConfigState(ctx)
	if err == nil {
		r.tracker.recordSuccess(ChanVsock)
		return res, "vsock", nil
	}
	r.tracker.recordFailure(ChanVsock, err)
	res, err = r.tcpGetConfigState(ctx)
	if err == nil {
		r.tracker.recordSuccess(ChanTCP)
		return res, "tcp", nil
	}
	r.tracker.recordFailure(ChanTCP, err)
	return nil, "", fmt.Errorf("GetConfigState: all channels failed")
}

// ---------------------------------------------------------------------------
// BGP route control: vsock -> TCP fallback (same pattern as GetConfigState)
// ---------------------------------------------------------------------------

func (r *RealOps) vsockGetBGPStatus(
	ctx context.Context,
) (*mwanv1.GetBGPStatusResponse, error) {
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
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cli := mwanv1.NewMWANAgentClient(conn)
	return cli.GetBGPStatus(cctx, &mwanv1.GetBGPStatusRequest{})
}

func (r *RealOps) tcpGetBGPStatus(
	ctx context.Context,
) (*mwanv1.GetBGPStatusResponse, error) {
	if r.tcpAddr == "" {
		return nil, fmt.Errorf("tcpGetBGPStatus: no tcp addr configured")
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
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cli := mwanv1.NewMWANAgentClient(conn)
	return cli.GetBGPStatus(cctx, &mwanv1.GetBGPStatusRequest{})
}

func (r *RealOps) GetBGPStatus(
	ctx context.Context, vmid string,
) (*mwanv1.GetBGPStatusResponse, error) {
	_ = vmid
	res, err := r.vsockGetBGPStatus(ctx)
	if err == nil {
		r.tracker.recordSuccess(ChanVsock)
		return res, nil
	}
	r.tracker.recordFailure(ChanVsock, err)
	res, err = r.tcpGetBGPStatus(ctx)
	if err == nil {
		r.tracker.recordSuccess(ChanTCP)
		return res, nil
	}
	r.tracker.recordFailure(ChanTCP, err)
	return nil, fmt.Errorf("GetBGPStatus: all channels failed")
}

func (r *RealOps) vsockAnnounceRoutes(
	ctx context.Context,
) (*mwanv1.AnnounceRoutesResponse, error) {
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
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cli := mwanv1.NewMWANAgentClient(conn)
	return cli.AnnounceRoutes(cctx, &mwanv1.AnnounceRoutesRequest{})
}

func (r *RealOps) tcpAnnounceRoutes(
	ctx context.Context,
) (*mwanv1.AnnounceRoutesResponse, error) {
	if r.tcpAddr == "" {
		return nil, fmt.Errorf("tcpAnnounceRoutes: no tcp addr configured")
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
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cli := mwanv1.NewMWANAgentClient(conn)
	return cli.AnnounceRoutes(cctx, &mwanv1.AnnounceRoutesRequest{})
}

func (r *RealOps) AnnounceRoutes(ctx context.Context, vmid string) error {
	_ = vmid
	res, err := r.vsockAnnounceRoutes(ctx)
	if err == nil {
		r.tracker.recordSuccess(ChanVsock)
		if !res.GetSuccess() {
			return fmt.Errorf("AnnounceRoutes: agent returned error: %s", res.GetError())
		}
		return nil
	}
	r.tracker.recordFailure(ChanVsock, err)
	res, err = r.tcpAnnounceRoutes(ctx)
	if err == nil {
		r.tracker.recordSuccess(ChanTCP)
		if !res.GetSuccess() {
			return fmt.Errorf("AnnounceRoutes: agent returned error: %s", res.GetError())
		}
		return nil
	}
	r.tracker.recordFailure(ChanTCP, err)
	return fmt.Errorf("AnnounceRoutes: all channels failed")
}

func (r *RealOps) vsockWithdrawRoutes(
	ctx context.Context,
) (*mwanv1.WithdrawRoutesResponse, error) {
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
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cli := mwanv1.NewMWANAgentClient(conn)
	return cli.WithdrawRoutes(cctx, &mwanv1.WithdrawRoutesRequest{})
}

func (r *RealOps) tcpWithdrawRoutes(
	ctx context.Context,
) (*mwanv1.WithdrawRoutesResponse, error) {
	if r.tcpAddr == "" {
		return nil, fmt.Errorf("tcpWithdrawRoutes: no tcp addr configured")
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
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cli := mwanv1.NewMWANAgentClient(conn)
	return cli.WithdrawRoutes(cctx, &mwanv1.WithdrawRoutesRequest{})
}

func (r *RealOps) WithdrawRoutes(ctx context.Context, vmid string) error {
	_ = vmid
	res, err := r.vsockWithdrawRoutes(ctx)
	if err == nil {
		r.tracker.recordSuccess(ChanVsock)
		if !res.GetSuccess() {
			return fmt.Errorf("WithdrawRoutes: agent returned error: %s", res.GetError())
		}
		return nil
	}
	r.tracker.recordFailure(ChanVsock, err)
	res, err = r.tcpWithdrawRoutes(ctx)
	if err == nil {
		r.tracker.recordSuccess(ChanTCP)
		if !res.GetSuccess() {
			return fmt.Errorf("WithdrawRoutes: agent returned error: %s", res.GetError())
		}
		return nil
	}
	r.tracker.recordFailure(ChanTCP, err)
	return fmt.Errorf("WithdrawRoutes: all channels failed")
}

func (r *RealOps) Ping(ctx context.Context, bin, target string) bool {
	cctx, cancel := context.WithTimeout(ctx, timeoutHostProbe)
	defer cancel()
	return exec.CommandContext(cctx, bin, "-c", "2", "-W", "3", target).Run() == nil
}

func (r *RealOps) SendEmail(ctx context.Context, to, subject, body string) error {
	return r.email.Send(ctx, to, subject, body)
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

// ExtractTracker returns the internal channel tracker for testing or diagnostics.
func (r *RealOps) ExtractTracker() *ChannelTracker {
	return r.tracker
}

// ---------------------------------------------------------------------------
// DryRunOps: wraps SysOps, logs destructive operations instead of executing
// ---------------------------------------------------------------------------

type DryRunOps struct {
	inner SysOps
	log   *slog.Logger
}

// NewDryRunOps creates a new dry-run wrapper.
func NewDryRunOps(inner SysOps, log *slog.Logger) *DryRunOps {
	return &DryRunOps{inner: inner, log: log}
}

func (d *DryRunOps) VMStatus(ctx context.Context, vmid string) (bool, error) {
	return d.inner.VMStatus(ctx, vmid)
}

func (d *DryRunOps) VMStop(_ context.Context, vmid string) error {
	d.log.Info("[DRY-RUN] would stop VM", "vmid", vmid)
	return nil
}

func (d *DryRunOps) VMRollback(_ context.Context, vmid, snap string) error {
	d.log.Info("[DRY-RUN] would rollback VM", "vmid", vmid, "snapshot", snap)
	return nil
}

func (d *DryRunOps) VMStart(_ context.Context, vmid string) error {
	d.log.Info("[DRY-RUN] would start VM", "vmid", vmid)
	return nil
}

func (d *DryRunOps) VMSnapshots(ctx context.Context, vmid string) ([]byte, error) {
	return d.inner.VMSnapshots(ctx, vmid)
}

func (d *DryRunOps) VMSnapshot(_ context.Context, vmid, snapName string) error {
	d.log.Info(
		"[DRY-RUN] would snapshot VM",
		"vmid", vmid,
		"snapshot", snapName,
	)
	return nil
}

func (d *DryRunOps) VMDelSnapshot(_ context.Context, vmid, snapName string) error {
	d.log.Info(
		"[DRY-RUN] would delete snapshot",
		"vmid", vmid,
		"snapshot", snapName,
	)
	return nil
}

func (d *DryRunOps) GuestExec(
	ctx context.Context, vmid string, args ...string,
) (GuestExecResult, error) {
	return d.inner.GuestExec(ctx, vmid, args...)
}

func (d *DryRunOps) Ping(ctx context.Context, bin, target string) bool {
	return d.inner.Ping(ctx, bin, target)
}

func (d *DryRunOps) SendEmail(_ context.Context, to, subject, _ string) error {
	d.log.Info(
		"[DRY-RUN] would send email",
		"to", to,
		"subject", subject,
	)
	return nil
}

func (d *DryRunOps) GetConfigState(
	ctx context.Context, vmid string,
) (*mwanv1.GetConfigStateResponse, string, error) {
	return d.inner.GetConfigState(ctx, vmid)
}

func (d *DryRunOps) GetBGPStatus(
	ctx context.Context, vmid string,
) (*mwanv1.GetBGPStatusResponse, error) {
	d.log.Info("[DRY-RUN] would get BGP status", "vmid", vmid)
	return &mwanv1.GetBGPStatusResponse{}, nil
}

func (d *DryRunOps) AnnounceRoutes(_ context.Context, vmid string) error {
	d.log.Info("[DRY-RUN] would announce BGP routes", "vmid", vmid)
	return nil
}

func (d *DryRunOps) WithdrawRoutes(_ context.Context, vmid string) error {
	d.log.Info("[DRY-RUN] would withdraw BGP routes", "vmid", vmid)
	return nil
}
