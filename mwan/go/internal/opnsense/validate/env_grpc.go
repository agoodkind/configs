package validate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"goodkind.io/mwan/internal/opnsense"
)

// ExecResult is the local mirror of opnsense.ExecResult so tests can
// satisfy OPNsenseRPCClient without importing the parent package.
type ExecResult = opnsense.ExecResult

// OPNsenseRPCClient is the narrow surface the gRPC Env needs from the
// mwan-opnsense daemon. It is satisfied by *opnsense.RPC in production
// and by an in-memory mock in tests. Keeping the interface local to
// the validate package avoids a dependency cycle and lets unit tests
// drive the gRPC path with a simple struct.
type OPNsenseRPCClient interface {
	// Exec runs a command inside the OPNsense guest via the bidi
	// Exec stream. The wrapper drives the stream synchronously and
	// returns a buffered result.
	Exec(ctx context.Context, command string, args []string, sudo bool, timeoutSeconds int32, stdin []byte) (*ExecResult, error)
}

// GRPCEnv is the OOB Env. Its OPNsense-side methods route through the
// mwan-opnsense daemon over the persistent virtio-serial gRPC channel
// instead of an SSH transport that pf may block. ProxmoxHost and
// LANClient methods delegate to an embedded ExecEnv because SSH to the
// Proxmox host and to LAN clients is not blocked by pf and the daemon
// has no RPC for those surfaces.
//
// The HTTPS GET path against the OPNsense web UI is proxied as an
// in-guest curl invoked via the same Exec RPC. The mwan-opnsense
// daemon does not yet expose a dedicated HTTP-proxy RPC, so curl-on-
// the-guest is the smallest path that lets every Env method work over
// gRPC. Filed as a follow-up: a typed HTTPRequest RPC would let the
// daemon return the parsed response without parsing curl stdout.
type GRPCEnv struct {
	// RPC is the typed mwan-opnsense client. Required.
	RPC OPNsenseRPCClient

	// Fallback is the SSH-based Env used for ProxmoxHost and
	// LANClient methods. Required when checks need those surfaces.
	Fallback *ExecEnv

	// ExecTimeoutSeconds caps each Exec RPC's per-call timeout.
	// Zero falls back to the daemon's default (30s); negative is
	// rejected at call time. Maximum honoured by the daemon is
	// 300 (5 minutes).
	ExecTimeoutSeconds int32

	// Clock indirects time. nil falls back to realClock.
	Clock clock
}

// SSHOPNsense runs a shell command inside the OPNsense guest by
// dispatching it to the mwan-opnsense Exec RPC wrapped in /bin/sh -c.
func (g *GRPCEnv) SSHOPNsense(ctx context.Context, command string) (CommandResult, error) {
	if g.RPC == nil {
		err := errors.New("GRPCEnv: RPC client not set")
		slog.ErrorContext(ctx, "validate GRPCEnv missing RPC client",
			"err", err.Error())
		return CommandResult{}, err
	}
	return g.execShell(ctx, command)
}

// SSHProxmoxHost delegates to the configured fallback ExecEnv.
func (g *GRPCEnv) SSHProxmoxHost(ctx context.Context, command string) (CommandResult, error) {
	if g.Fallback == nil {
		err := errors.New("GRPCEnv: Fallback ExecEnv not set; SSHProxmoxHost unavailable")
		slog.ErrorContext(ctx, "validate GRPCEnv missing fallback for SSHProxmoxHost",
			"err", err.Error())
		return CommandResult{}, err
	}
	return g.Fallback.SSHProxmoxHost(ctx, command)
}

// LANClientExec delegates to the configured fallback ExecEnv.
func (g *GRPCEnv) LANClientExec(ctx context.Context, command string) (CommandResult, error) {
	if g.Fallback == nil {
		err := errors.New("GRPCEnv: Fallback ExecEnv not set; LANClientExec unavailable")
		slog.ErrorContext(ctx, "validate GRPCEnv missing fallback for LANClientExec",
			"err", err.Error())
		return CommandResult{}, err
	}
	return g.Fallback.LANClientExec(ctx, command)
}

// OPNsenseHTTPSGet fetches a URL on the OPNsense web UI by running
// curl inside the guest via the Exec RPC. The response status code
// is appended via curl's -w trailer and parsed back out so the
// returned HTTPResult shape matches the SSH-free ExecEnv.
func (g *GRPCEnv) OPNsenseHTTPSGet(
	ctx context.Context,
	path string,
	auth *BasicAuth,
) (HTTPResult, error) {
	if g.RPC == nil {
		err := errors.New("GRPCEnv: RPC client not set")
		slog.ErrorContext(ctx, "validate GRPCEnv missing RPC client for HTTPS",
			"err", err.Error())
		return HTTPResult{}, err
	}
	cmd := buildInGuestCurlCommand(path, auth)
	res, err := g.execShell(ctx, cmd)
	if err != nil {
		return HTTPResult{}, err
	}
	if res.ExitCode != 0 {
		err := fmt.Errorf("in-guest curl exit %d: %s",
			res.ExitCode, strings.TrimSpace(res.Stderr))
		slog.ErrorContext(ctx, "validate GRPCEnv curl exit non-zero",
			"path", path, "exit", res.ExitCode, "err", err.Error())
		return HTTPResult{}, err
	}
	return parseCurlOutput(res.Stdout)
}

// Now returns the current time via the configured clock.
func (g *GRPCEnv) Now() time.Time {
	if g.Clock != nil {
		return g.Clock.Now()
	}
	return realClock{}.Now()
}

// execShell wraps a shell string in /bin/sh -c and dispatches to the
// daemon's Exec RPC. Stdout and stderr are returned as strings; the
// exit code is forwarded so non-zero exits surface as fail outcomes
// rather than transport errors. Truncation flags are folded into
// stderr so the operator notices the dropped bytes without the Env
// surface needing a separate field.
func (g *GRPCEnv) execShell(ctx context.Context, command string) (CommandResult, error) {
	resp, err := g.RPC.Exec(ctx, "/bin/sh", []string{"-c", command}, false, g.ExecTimeoutSeconds, nil)
	if err != nil {
		slog.ErrorContext(ctx, "validate GRPCEnv Exec RPC failed", "err", err.Error())
		return CommandResult{}, fmt.Errorf("grpc Exec: %w", err)
	}
	if resp == nil {
		err := errors.New("GRPCEnv: nil ExecResult from daemon")
		slog.ErrorContext(ctx, "validate GRPCEnv nil response", "err", err.Error())
		return CommandResult{}, err
	}
	stderrText := string(resp.Stderr)
	if resp.TimedOut {
		stderrText = appendDiag(stderrText, "[grpc-env: command timed out]")
	}
	if resp.StdoutTruncated {
		stderrText = appendDiag(stderrText, "[grpc-env: stdout truncated at 10 MB]")
	}
	if resp.StderrTruncated {
		stderrText = appendDiag(stderrText, "[grpc-env: stderr truncated at 10 MB]")
	}
	return CommandResult{
		Stdout:   string(resp.Stdout),
		Stderr:   stderrText,
		ExitCode: int(resp.ExitCode),
	}, nil
}

// appendDiag joins a diagnostic line onto an existing stderr blob
// without colliding with trailing whitespace.
func appendDiag(stderr, diag string) string {
	if stderr == "" {
		return diag
	}
	if strings.HasSuffix(stderr, "\n") {
		return stderr + diag
	}
	return stderr + "\n" + diag
}

// buildInGuestCurlCommand returns the shell command run inside the
// OPNsense guest to fetch a URL on the local web UI. The trailing
// "\n<status>" line is parsed by parseCurlOutput. -k is required
// because the web UI cert is self-signed; trust comes from API auth.
func buildInGuestCurlCommand(path string, auth *BasicAuth) string {
	parts := []string{
		"curl", "-sS", "-k",
		"--max-time", "5",
		"-o", "-",
		"-w", "'\\n%{http_code}'",
	}
	if auth != nil {
		parts = append(parts,
			"-u", shellSingleQuote(auth.Username+":"+auth.Password))
	}
	parts = append(parts, shellSingleQuote("https://127.0.0.1:443"+path))
	return strings.Join(parts, " ")
}

// shellSingleQuote wraps s in single quotes, escaping embedded ones.
// The OPNsense base image ships /bin/sh as ash-compatible, so single
// quotes are the safest grouping primitive.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// parseCurlOutput peels the trailing "\n<status>" written by curl -w
// off the response body. The body may legitimately contain newlines,
// so we split from the right.
func parseCurlOutput(stdout string) (HTTPResult, error) {
	idx := strings.LastIndex(stdout, "\n")
	if idx < 0 {
		err := fmt.Errorf("curl output missing status trailer: %q", stdout)
		slog.Error("validate GRPCEnv parseCurlOutput missing trailer",
			"err", err.Error())
		return HTTPResult{}, err
	}
	body := stdout[:idx]
	statusStr := strings.TrimSpace(stdout[idx+1:])
	status, err := strconv.Atoi(statusStr)
	if err != nil {
		wrapped := fmt.Errorf("parse curl status %q: %w", statusStr, err)
		slog.Error("validate GRPCEnv parseCurlOutput bad status",
			"err", wrapped.Error())
		return HTTPResult{}, wrapped
	}
	return HTTPResult{StatusCode: status, Body: body}, nil
}

// compile-time check that GRPCEnv satisfies Env.
var _ Env = (*GRPCEnv)(nil)
