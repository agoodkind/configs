package upgrade

import (
	"context"
	"errors"
	"strings"
	"testing"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// mockOPNsenseRPCClient is a deterministic stand-in for the
// mwan-opnsense daemon. It records every Exec call and returns a canned
// response keyed by argv[0]. This mirrors the fakeExec helper in
// upgrade_test.go but speaks the gRPC ExecRequest/ExecResponse shape.
type mockOPNsenseRPCClient struct {
	calls    []*mwanv1.ExecRequest
	response *mwanv1.ExecResponse
	err      error
}

func (m *mockOPNsenseRPCClient) Exec(
	_ context.Context, req *mwanv1.ExecRequest,
) (*mwanv1.ExecResponse, error) {
	m.calls = append(m.calls, req)
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// TestGRPCExecutorMapsArgvToExecRequest confirms the first argv element
// becomes ExecRequest.Command and the rest become ExecRequest.Args. The
// upgrade prepare phase issues commands like ("cat", "/conf/config.xml")
// and ("vtysh", "-c", "show bgp summary json") so this mapping is
// load-bearing for the artefact capture path.
func TestGRPCExecutorMapsArgvToExecRequest(t *testing.T) {
	t.Parallel()
	mock := &mockOPNsenseRPCClient{
		response: &mwanv1.ExecResponse{
			Stdout:   []byte("<config/>"),
			Stderr:   nil,
			ExitCode: 0,
		},
	}
	exec := &GRPCExecutor{RPC: mock, ExecTimeoutSeconds: 0}
	res, err := exec.GuestExec(
		context.Background(), "102", "vtysh", "-c", "show bgp summary json",
	)
	if err != nil {
		t.Fatalf("GuestExec: unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if res.Stdout != "<config/>" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "<config/>")
	}
	if len(mock.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(mock.calls))
	}
	got := mock.calls[0]
	if got.GetCommand() != "vtysh" {
		t.Errorf("Command = %q, want %q", got.GetCommand(), "vtysh")
	}
	gotArgs := got.GetArgs()
	if len(gotArgs) != 2 || gotArgs[0] != "-c" || gotArgs[1] != "show bgp summary json" {
		t.Errorf("Args = %v, want [-c show bgp summary json]", gotArgs)
	}
	if got.GetSudo() {
		t.Error("Sudo = true, want false")
	}
}

// TestGRPCExecutorPropagatesExitCodeAndStreams asserts that exit code,
// stdout, and stderr from the daemon land verbatim on the returned
// GuestExecResult. The execute phase logs all three to upgrade.log and
// the prepare phase reads stdout into the artefact files, so each must
// pass through unchanged.
func TestGRPCExecutorPropagatesExitCodeAndStreams(t *testing.T) {
	t.Parallel()
	mock := &mockOPNsenseRPCClient{
		response: &mwanv1.ExecResponse{
			Stdout:   []byte("partial output"),
			Stderr:   []byte("warning: thing\n"),
			ExitCode: 7,
		},
	}
	exec := &GRPCExecutor{RPC: mock}
	res, err := exec.GuestExec(context.Background(), "102", "opnsense-upgrade", "-c")
	if err != nil {
		t.Fatalf("GuestExec: unexpected error: %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if res.Stdout != "partial output" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "partial output")
	}
	if res.Stderr != "warning: thing\n" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "warning: thing\n")
	}
}

// TestGRPCExecutorAnnotatesTimeout confirms the daemon's timed_out flag
// is surfaced on stderr so the operator log carries the diagnostic
// without the upgrade package needing a dedicated bool field.
func TestGRPCExecutorAnnotatesTimeout(t *testing.T) {
	t.Parallel()
	mock := &mockOPNsenseRPCClient{
		response: &mwanv1.ExecResponse{
			Stdout:   []byte(""),
			Stderr:   []byte(""),
			ExitCode: 124,
			TimedOut: true,
		},
	}
	exec := &GRPCExecutor{RPC: mock}
	res, err := exec.GuestExec(context.Background(), "102", "opnsense-upgrade", "-r", "26.1.7")
	if err != nil {
		t.Fatalf("GuestExec: unexpected error: %v", err)
	}
	if res.ExitCode != 124 {
		t.Errorf("ExitCode = %d, want 124", res.ExitCode)
	}
	if res.Stderr != "[grpc-executor: command timed out]" {
		t.Errorf("Stderr = %q, want timeout diagnostic", res.Stderr)
	}
}

// TestGRPCExecutorAnnotatesTruncation confirms truncation flags from the
// daemon land on stderr. The 10 MB cap matters for the config.xml
// capture path on a hostile guest.
func TestGRPCExecutorAnnotatesTruncation(t *testing.T) {
	t.Parallel()
	mock := &mockOPNsenseRPCClient{
		response: &mwanv1.ExecResponse{
			Stdout:          []byte("clipped"),
			Stderr:          []byte("noise"),
			ExitCode:        0,
			StdoutTruncated: true,
			StderrTruncated: true,
		},
	}
	exec := &GRPCExecutor{RPC: mock}
	res, err := exec.GuestExec(context.Background(), "102", "cat", "/conf/config.xml")
	if err != nil {
		t.Fatalf("GuestExec: unexpected error: %v", err)
	}
	wantSubs := []string{
		"noise",
		"[grpc-executor: stdout truncated at 10 MB]",
		"[grpc-executor: stderr truncated at 10 MB]",
	}
	for _, sub := range wantSubs {
		if !strings.Contains(res.Stderr, sub) {
			t.Errorf("Stderr %q missing %q", res.Stderr, sub)
		}
	}
}

// TestGRPCExecutorRejectsNilRPC confirms an unconfigured executor errors
// fast rather than panicking on a nil pointer.
func TestGRPCExecutorRejectsNilRPC(t *testing.T) {
	t.Parallel()
	exec := &GRPCExecutor{RPC: nil}
	_, err := exec.GuestExec(context.Background(), "102", "true")
	if err == nil {
		t.Fatal("expected error for nil RPC, got nil")
	}
}

// TestGRPCExecutorRejectsEmptyArgv confirms callers cannot bypass the
// Command field. The Executor contract treats argv[0] as the binary, so
// an empty argv has no defined behaviour and is rejected.
func TestGRPCExecutorRejectsEmptyArgv(t *testing.T) {
	t.Parallel()
	mock := &mockOPNsenseRPCClient{response: &mwanv1.ExecResponse{}}
	exec := &GRPCExecutor{RPC: mock}
	_, err := exec.GuestExec(context.Background(), "102")
	if err == nil {
		t.Fatal("expected error for empty argv, got nil")
	}
	if len(mock.calls) != 0 {
		t.Errorf("len(calls) = %d, want 0; RPC should not fire on empty argv", len(mock.calls))
	}
}

// TestGRPCExecutorWrapsTransportError asserts the daemon's error is
// wrapped so callers can use errors.Is for assertion. The execute phase
// inspects errors.Is against context.DeadlineExceeded for the watchdog
// hung-state distinction; that lookup must keep working when the
// transport is gRPC.
func TestGRPCExecutorWrapsTransportError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("daemon: socket closed")
	mock := &mockOPNsenseRPCClient{err: wantErr}
	exec := &GRPCExecutor{RPC: mock}
	_, err := exec.GuestExec(context.Background(), "102", "true")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want chain containing %v", err, wantErr)
	}
}

// TestGRPCExecutorRejectsNilResponse covers the daemon-misbehaviour
// path: a nil response without an explicit error must surface as an
// error rather than a zero-valued GuestExecResult.
func TestGRPCExecutorRejectsNilResponse(t *testing.T) {
	t.Parallel()
	mock := &mockOPNsenseRPCClient{response: nil, err: nil}
	exec := &GRPCExecutor{RPC: mock}
	_, err := exec.GuestExec(context.Background(), "102", "true")
	if err == nil {
		t.Fatal("expected error for nil ExecResponse, got nil")
	}
}

// TestGRPCExecutorPassesTimeoutToDaemon confirms ExecTimeoutSeconds is
// forwarded on the request so the daemon's per-call timeout uses the
// operator-supplied value.
func TestGRPCExecutorPassesTimeoutToDaemon(t *testing.T) {
	t.Parallel()
	mock := &mockOPNsenseRPCClient{response: &mwanv1.ExecResponse{}}
	exec := &GRPCExecutor{RPC: mock, ExecTimeoutSeconds: 60}
	_, err := exec.GuestExec(context.Background(), "102", "opnsense-version")
	if err != nil {
		t.Fatalf("GuestExec: unexpected error: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(mock.calls))
	}
	if mock.calls[0].GetTimeoutSeconds() != 60 {
		t.Errorf("TimeoutSeconds = %d, want 60", mock.calls[0].GetTimeoutSeconds())
	}
}
