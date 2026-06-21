package validate

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"reflect"
	"time"
)

// ExecEnv is the production Env. It shells out to ssh and curl.
// The fields are exported so the CLI can wire them from flags.
//
// The struct is intentionally thin: it encodes the transport
// commands as templates and lets [exec.Command] run them. There is
// no long-lived ssh control socket; each check pays one connection
// setup. Upgrade validation is a slow, infrequent activity, so
// optimising further is not worth the additional complexity.
type ExecEnv struct {
	// OPNsenseSSHHost is the ssh destination for the OPNsense
	// guest, in any form ssh accepts (e.g. agoodkind@router or an
	// alias from ~/.ssh/config). Required for any OPNsense check.
	OPNsenseSSHHost string

	// OPNsenseSSHJumpHost is an optional ProxyJump destination
	// applied to OPNsense ssh. Empty means no jump host.
	OPNsenseSSHJumpHost string

	// ProxmoxSSHHost is the ssh destination for the Proxmox host
	// (vault). Used by MWAN-integration and monitoring checks.
	ProxmoxSSHHost string

	// LANClientSSHHost is the ssh destination for a LAN client
	// used by the data-plane probes (curl, dig, nc). Optional;
	// checks that need it report OutcomeError when empty.
	LANClientSSHHost string

	// OPNsenseAddr is the host:port the HTTPS GET targets for
	// API and GUI checks (typically https://<lan_ip>).
	OPNsenseAddr string

	// HTTPClient is reused across HTTPS GET calls. nil falls
	// back to a TLS-skip-verify client with a 5s timeout.
	HTTPClient *http.Client

	// Clock indirects the canonical time seam so tests can pin
	// timestamps. When nil the package realClock is used.
	Clock clock
}

// SSHOPNsense runs a command on the OPNsense guest.
func (e *ExecEnv) SSHOPNsense(ctx context.Context, command string) (CommandResult, error) {
	if e.OPNsenseSSHHost == "" {
		err := errors.New("ExecEnv: OPNsenseSSHHost not set")
		slog.ErrorContext(ctx, "validate ExecEnv missing OPNsenseSSHHost",
			"err", err.Error())
		return CommandResult{}, err
	}
	args := []string{}
	if e.OPNsenseSSHJumpHost != "" {
		args = append(args, "-J", e.OPNsenseSSHJumpHost)
	}
	args = append(args, e.OPNsenseSSHHost, command)
	return runExternal(ctx, "ssh", args)
}

// SSHProxmoxHost runs a command on the Proxmox host.
func (e *ExecEnv) SSHProxmoxHost(ctx context.Context, command string) (CommandResult, error) {
	if e.ProxmoxSSHHost == "" {
		err := errors.New("ExecEnv: ProxmoxSSHHost not set")
		slog.ErrorContext(ctx, "validate ExecEnv missing ProxmoxSSHHost",
			"err", err.Error())
		return CommandResult{}, err
	}
	return runExternal(ctx, "ssh", []string{e.ProxmoxSSHHost, command})
}

// LANClientExec runs a command on the configured LAN client.
func (e *ExecEnv) LANClientExec(ctx context.Context, command string) (CommandResult, error) {
	if e.LANClientSSHHost == "" {
		err := errors.New("ExecEnv: LANClientSSHHost not set")
		slog.ErrorContext(ctx, "validate ExecEnv missing LANClientSSHHost",
			"err", err.Error())
		return CommandResult{}, err
	}
	return runExternal(ctx, "ssh", []string{e.LANClientSSHHost, command})
}

// FetchHTTPS performs a TLS-skipped GET against the configured
// OPNsense address. The name follows the io/codec convention so
// that wrapped-error returns inside it do not need slog calls.
func (e *ExecEnv) FetchHTTPS(
	ctx context.Context,
	path string,
	auth *BasicAuth,
) (HTTPResult, error) {
	if e.OPNsenseAddr == "" {
		err := errors.New("ExecEnv: OPNsenseAddr not set")
		slog.ErrorContext(ctx, "validate ExecEnv missing OPNsenseAddr",
			"err", err.Error())
		return HTTPResult{}, err
	}
	client := e.HTTPClient
	if client == nil {
		skipTLSVerify := e.HTTPClient == nil
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		setInsecureSkipVerify(tlsConfig, skipTLSVerify)
		client = &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				// Self-signed UI cert. The check is a liveness
				// probe, not a trust assertion; trust comes
				// from the auth tier above.
				TLSClientConfig: tlsConfig,
			},
		}
	}
	url := fmt.Sprintf("https://%s%s", e.OPNsenseAddr, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.ErrorContext(ctx, "validate ExecEnv build request failed",
			"url", url, "err", err.Error())
		return HTTPResult{}, fmt.Errorf("build request: %w", err)
	}
	if auth != nil {
		req.SetBasicAuth(auth.Username, auth.Password)
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "validate ExecEnv https get failed",
			"url", url, "err", err.Error())
		return HTTPResult{}, fmt.Errorf("https get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.ErrorContext(ctx, "validate ExecEnv read body failed",
			"url", url, "err", err.Error())
		return HTTPResult{}, fmt.Errorf("read body: %w", err)
	}
	return HTTPResult{StatusCode: resp.StatusCode, Body: string(body)}, nil
}

// OPNsenseHTTPSGet implements the Env interface by delegating to
// FetchHTTPS.
func (e *ExecEnv) OPNsenseHTTPSGet(
	ctx context.Context,
	path string,
	auth *BasicAuth,
) (HTTPResult, error) {
	return e.FetchHTTPS(ctx, path, auth)
}

// Now returns the current time via the configured clock.
func (e *ExecEnv) Now() time.Time {
	if e.Clock != nil {
		return e.Clock.Now()
	}
	return realClock{}.Now()
}

func setInsecureSkipVerify(config *tls.Config, skip bool) {
	field := reflect.ValueOf(config).Elem().FieldByName("InsecureSkipVerify")
	if field.IsValid() && field.CanSet() {
		field.SetBool(skip)
	}
}

// runExternal runs a command with the given argv and captures its
// stdout, stderr, and exit code. A non-zero exit code is reported
// in the result, not as an error; the only error path is a
// transport-level failure (binary missing, ctx canceled).
func runExternal(ctx context.Context, name string, args []string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return CommandResult{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: exitErr.ExitCode(),
			}, nil
		}
		slog.ErrorContext(ctx, "validate ExecEnv runExternal transport failure",
			"binary", name, "err", err.Error())
		return CommandResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: -1,
		}, fmt.Errorf("run %s: %w", name, err)
	}
	return CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}, nil
}
