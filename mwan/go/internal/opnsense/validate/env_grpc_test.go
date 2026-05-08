package validate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// fakeRPC is a minimal OPNsenseRPCClient mock. It records the last
// request and returns a scripted response or error.
type fakeRPC struct {
	lastReq *mwanv1.ExecRequest
	resp    *mwanv1.ExecResponse
	err     error
	calls   int
}

func (f *fakeRPC) Exec(_ context.Context, req *mwanv1.ExecRequest) (*mwanv1.ExecResponse, error) {
	f.calls++
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func TestGRPCEnv_SSHOPNsense_WrapsCommandInShell(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil,
		resp: &mwanv1.ExecResponse{
			Stdout:          []byte("ok\n"),
			Stderr:          nil,
			ExitCode:        0,
			DurationMs:      1,
			StdoutTruncated: false,
			StderrTruncated: false,
			TimedOut:        false,
		},
		err:   nil,
		calls: 0,
	}
	env := &GRPCEnv{
		RPC:                rpc,
		Fallback:           nil,
		ExecTimeoutSeconds: 12,
		Clock:              nil,
	}

	got, err := env.SSHOPNsense(context.Background(), "vtysh -c 'show bgp ipv4 unicast summary json'")
	if err != nil {
		t.Fatalf("SSHOPNsense returned err: %v", err)
	}
	if got.Stdout != "ok\n" {
		t.Fatalf("Stdout=%q want %q", got.Stdout, "ok\n")
	}
	if got.ExitCode != 0 {
		t.Fatalf("ExitCode=%d want 0", got.ExitCode)
	}

	if rpc.lastReq.GetCommand() != "/bin/sh" {
		t.Fatalf("Command=%q want /bin/sh", rpc.lastReq.GetCommand())
	}
	args := rpc.lastReq.GetArgs()
	if len(args) != 2 || args[0] != "-c" {
		t.Fatalf("Args=%v want [-c <command>]", args)
	}
	if !strings.Contains(args[1], "vtysh") {
		t.Fatalf("Args[1]=%q does not contain vtysh", args[1])
	}
	if rpc.lastReq.GetTimeoutSeconds() != 12 {
		t.Fatalf("TimeoutSeconds=%d want 12", rpc.lastReq.GetTimeoutSeconds())
	}
}

func TestGRPCEnv_SSHOPNsense_NonZeroExitNotError(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil,
		resp: &mwanv1.ExecResponse{
			Stdout:          []byte(""),
			Stderr:          []byte("boom"),
			ExitCode:        2,
			DurationMs:      1,
			StdoutTruncated: false,
			StderrTruncated: false,
			TimedOut:        false,
		},
		err:   nil,
		calls: 0,
	}
	env := &GRPCEnv{RPC: rpc, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}

	got, err := env.SSHOPNsense(context.Background(), "false")
	if err != nil {
		t.Fatalf("SSHOPNsense surfaced exit code as err: %v", err)
	}
	if got.ExitCode != 2 {
		t.Fatalf("ExitCode=%d want 2", got.ExitCode)
	}
	if got.Stderr != "boom" {
		t.Fatalf("Stderr=%q want boom", got.Stderr)
	}
}

func TestGRPCEnv_SSHOPNsense_TruncationFlagsAppendDiag(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil,
		resp: &mwanv1.ExecResponse{
			Stdout:          []byte("partial"),
			Stderr:          []byte("err1"),
			ExitCode:        0,
			DurationMs:      1,
			StdoutTruncated: true,
			StderrTruncated: true,
			TimedOut:        true,
		},
		err:   nil,
		calls: 0,
	}
	env := &GRPCEnv{RPC: rpc, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}

	got, err := env.SSHOPNsense(context.Background(), "true")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got.Stderr, "timed out") {
		t.Fatalf("Stderr missing timed out: %q", got.Stderr)
	}
	if !strings.Contains(got.Stderr, "stdout truncated") {
		t.Fatalf("Stderr missing stdout truncated: %q", got.Stderr)
	}
	if !strings.Contains(got.Stderr, "stderr truncated") {
		t.Fatalf("Stderr missing stderr truncated: %q", got.Stderr)
	}
}

func TestGRPCEnv_SSHOPNsense_TransportErrorPropagated(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil, resp: nil,
		err:   errors.New("daemon down"),
		calls: 0,
	}
	env := &GRPCEnv{RPC: rpc, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}

	_, err := env.SSHOPNsense(context.Background(), "true")
	if err == nil {
		t.Fatalf("want transport error, got nil")
	}
	if !strings.Contains(err.Error(), "daemon down") {
		t.Fatalf("err=%v missing daemon down", err)
	}
}

func TestGRPCEnv_SSHOPNsense_RequiresRPCClient(t *testing.T) {
	env := &GRPCEnv{RPC: nil, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}
	_, err := env.SSHOPNsense(context.Background(), "true")
	if err == nil {
		t.Fatalf("want error when RPC is nil")
	}
}

func TestGRPCEnv_OPNsenseHTTPSGet_Success(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil,
		resp: &mwanv1.ExecResponse{
			Stdout:          []byte("hello world\n200"),
			Stderr:          nil,
			ExitCode:        0,
			DurationMs:      1,
			StdoutTruncated: false,
			StderrTruncated: false,
			TimedOut:        false,
		},
		err:   nil,
		calls: 0,
	}
	env := &GRPCEnv{RPC: rpc, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}

	got, err := env.OPNsenseHTTPSGet(context.Background(), "/api/core/firmware/status",
		&BasicAuth{Username: "k", Password: "s"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.StatusCode != 200 {
		t.Fatalf("StatusCode=%d want 200", got.StatusCode)
	}
	if got.Body != "hello world" {
		t.Fatalf("Body=%q want hello world", got.Body)
	}
	args := rpc.lastReq.GetArgs()
	if len(args) != 2 {
		t.Fatalf("Args=%v want 2 entries", args)
	}
	if !strings.Contains(args[1], "curl") || !strings.Contains(args[1], "/api/core/firmware/status") {
		t.Fatalf("curl command missing pieces: %q", args[1])
	}
	if !strings.Contains(args[1], "-u 'k:s'") {
		t.Fatalf("auth pair not single-quoted in command: %q", args[1])
	}
}

func TestGRPCEnv_OPNsenseHTTPSGet_BodyWithNewlines(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil,
		resp: &mwanv1.ExecResponse{
			Stdout:          []byte("line1\nline2\nline3\n418"),
			Stderr:          nil,
			ExitCode:        0,
			DurationMs:      1,
			StdoutTruncated: false,
			StderrTruncated: false,
			TimedOut:        false,
		},
		err:   nil,
		calls: 0,
	}
	env := &GRPCEnv{RPC: rpc, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}

	got, err := env.OPNsenseHTTPSGet(context.Background(), "/", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.StatusCode != 418 {
		t.Fatalf("StatusCode=%d want 418", got.StatusCode)
	}
	if got.Body != "line1\nline2\nline3" {
		t.Fatalf("Body=%q want %q", got.Body, "line1\nline2\nline3")
	}
}

func TestGRPCEnv_OPNsenseHTTPSGet_CurlExitNonZeroIsError(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil,
		resp: &mwanv1.ExecResponse{
			Stdout:          []byte(""),
			Stderr:          []byte("could not resolve host"),
			ExitCode:        6,
			DurationMs:      1,
			StdoutTruncated: false,
			StderrTruncated: false,
			TimedOut:        false,
		},
		err:   nil,
		calls: 0,
	}
	env := &GRPCEnv{RPC: rpc, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}

	_, err := env.OPNsenseHTTPSGet(context.Background(), "/", nil)
	if err == nil {
		t.Fatalf("want error on curl exit 6")
	}
}

func TestGRPCEnv_OPNsenseHTTPSGet_MissingTrailerIsError(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil,
		resp: &mwanv1.ExecResponse{
			Stdout:          []byte("no trailer here"),
			Stderr:          nil,
			ExitCode:        0,
			DurationMs:      1,
			StdoutTruncated: false,
			StderrTruncated: false,
			TimedOut:        false,
		},
		err:   nil,
		calls: 0,
	}
	env := &GRPCEnv{RPC: rpc, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}

	_, err := env.OPNsenseHTTPSGet(context.Background(), "/", nil)
	if err == nil {
		t.Fatalf("want error on missing trailer")
	}
}

func TestGRPCEnv_OPNsenseHTTPSGet_NonNumericStatusIsError(t *testing.T) {
	rpc := &fakeRPC{
		lastReq: nil,
		resp: &mwanv1.ExecResponse{
			Stdout:          []byte("body\nNOTNUM"),
			Stderr:          nil,
			ExitCode:        0,
			DurationMs:      1,
			StdoutTruncated: false,
			StderrTruncated: false,
			TimedOut:        false,
		},
		err:   nil,
		calls: 0,
	}
	env := &GRPCEnv{RPC: rpc, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}

	_, err := env.OPNsenseHTTPSGet(context.Background(), "/", nil)
	if err == nil {
		t.Fatalf("want error on non-numeric status")
	}
}

func TestGRPCEnv_SSHProxmoxHost_RequiresFallback(t *testing.T) {
	env := &GRPCEnv{RPC: &fakeRPC{}, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}
	_, err := env.SSHProxmoxHost(context.Background(), "true")
	if err == nil {
		t.Fatalf("want error when Fallback is nil")
	}
}

func TestGRPCEnv_LANClientExec_RequiresFallback(t *testing.T) {
	env := &GRPCEnv{RPC: &fakeRPC{}, Fallback: nil, ExecTimeoutSeconds: 0, Clock: nil}
	_, err := env.LANClientExec(context.Background(), "true")
	if err == nil {
		t.Fatalf("want error when Fallback is nil")
	}
}

func TestGRPCEnv_Now_HonoursClock(t *testing.T) {
	pinned := time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC)
	env := &GRPCEnv{
		RPC:                &fakeRPC{},
		Fallback:           nil,
		ExecTimeoutSeconds: 0,
		Clock:              fixedClockGRPC{t: pinned},
	}
	if !env.Now().Equal(pinned) {
		t.Fatalf("Now=%v want %v", env.Now(), pinned)
	}
}

// fixedClockGRPC is a tiny clock seam local to this test file.
type fixedClockGRPC struct{ t time.Time }

func (f fixedClockGRPC) Now() time.Time { return f.t }
