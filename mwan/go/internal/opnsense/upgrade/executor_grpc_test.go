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

// TestGRPCExecutorPassesLongTimeoutToDaemon is the MWAN-177 regression
// guard. Rehearsal 5 showed that `opnsense-update -u` takes longer than
// the daemon's prior 5-minute hard cap. The factory wires
// --exec-timeout=60m through the executor as 3600s on the ExecRequest;
// this test pins that path so a future refactor cannot quietly drop the
// long timeout back to zero or to the pre-MWAN-177 cap.
func TestGRPCExecutorPassesLongTimeoutToDaemon(t *testing.T) {
	t.Parallel()
	mock := &mockOPNsenseRPCClient{response: &mwanv1.ExecResponse{}}
	const sixtyMinutesSeconds int32 = 60 * 60
	exec := &GRPCExecutor{RPC: mock, ExecTimeoutSeconds: sixtyMinutesSeconds}
	_, err := exec.GuestExec(
		context.Background(), "102", "opnsense-update", "-u",
	)
	if err != nil {
		t.Fatalf("GuestExec: unexpected error: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(mock.calls))
	}
	got := mock.calls[0].GetTimeoutSeconds()
	if got != sixtyMinutesSeconds {
		t.Errorf("TimeoutSeconds = %d, want %d", got, sixtyMinutesSeconds)
	}
}

// flakyClosedRPC returns a closed-client error on the first call and
// delegates to a successful response on the second call. It is the
// MWAN-178 mock: after `qm rollback`, the first Exec hits the dead
// virtio-serial channel; once the executor redials, the second call
// must land on a fresh client.
type flakyClosedRPC struct {
	calls    int
	response *mwanv1.ExecResponse
	closeErr error
}

func (f *flakyClosedRPC) Exec(
	_ context.Context, _ *mwanv1.ExecRequest,
) (*mwanv1.ExecResponse, error) {
	f.calls++
	if f.calls == 1 {
		return nil, f.closeErr
	}
	return f.response, nil
}

// TestGRPCExecutorRedialsOnClientClosed is the MWAN-178 regression
// guard. The post-rollback waitForGuest poll observed
// "opnsense: client is closed" on every retry because the orchestrator
// reused the gRPC client that the qm-rollback-induced QEMU restart had
// killed. The fix: on a client-closed error, the executor invokes the
// Redial closure to obtain a fresh RPC, then retries the Exec once.
func TestGRPCExecutorRedialsOnClientClosed(t *testing.T) {
	t.Parallel()
	closedErr := errors.New("opnsense: client is closed")
	freshRPC := &mockOPNsenseRPCClient{
		response: &mwanv1.ExecResponse{
			Stdout:   []byte("alive"),
			ExitCode: 0,
		},
	}
	staleRPC := &flakyClosedRPC{closeErr: closedErr}
	var redialCalls int
	exec := &GRPCExecutor{
		RPC: staleRPC,
		Redial: func() (OPNsenseRPCClient, error) {
			redialCalls++
			return freshRPC, nil
		},
	}
	res, err := exec.GuestExec(context.Background(), "102", "true")
	if err != nil {
		t.Fatalf("GuestExec: unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if res.Stdout != "alive" {
		t.Errorf("Stdout = %q, want alive (proves redial swapped clients)", res.Stdout)
	}
	if redialCalls != 1 {
		t.Errorf("redialCalls = %d, want 1", redialCalls)
	}
	if staleRPC.calls != 1 {
		t.Errorf("staleRPC.calls = %d, want 1 (only the initial closed call)", staleRPC.calls)
	}
	if len(freshRPC.calls) != 1 {
		t.Errorf("freshRPC.calls = %d, want 1 (the post-redial retry)", len(freshRPC.calls))
	}
}

// TestGRPCExecutorRedialDisabledWithoutClosure confirms the executor
// surfaces the client-closed error verbatim when Redial is nil. Tests
// and call sites that do not need redial behaviour see no behavioural
// change from MWAN-178.
func TestGRPCExecutorRedialDisabledWithoutClosure(t *testing.T) {
	t.Parallel()
	closedErr := errors.New("opnsense: client is closed")
	stale := &flakyClosedRPC{closeErr: closedErr}
	exec := &GRPCExecutor{RPC: stale, Redial: nil}
	_, err := exec.GuestExec(context.Background(), "102", "true")
	if err == nil {
		t.Fatal("expected client-closed error to surface, got nil")
	}
	if !errors.Is(err, closedErr) {
		t.Errorf("error chain = %v, want chain containing %v", err, closedErr)
	}
	if stale.calls != 1 {
		t.Errorf("stale.calls = %d, want 1 (no retry without Redial)", stale.calls)
	}
}

// TestGRPCExecutorRedialFailureSurfacesError covers the path where the
// reconnect itself fails. The original Exec error and the redial error
// must both be visible so the operator log captures the full failure.
func TestGRPCExecutorRedialFailureSurfacesError(t *testing.T) {
	t.Parallel()
	closedErr := errors.New("opnsense: client is closed")
	dialErr := errors.New("dial: socket missing")
	stale := &flakyClosedRPC{closeErr: closedErr}
	exec := &GRPCExecutor{
		RPC: stale,
		Redial: func() (OPNsenseRPCClient, error) {
			return nil, dialErr
		},
	}
	_, err := exec.GuestExec(context.Background(), "102", "true")
	if err == nil {
		t.Fatal("expected redial failure to surface, got nil")
	}
	if !errors.Is(err, dialErr) {
		t.Errorf("error chain = %v, want chain containing %v", err, dialErr)
	}
}

// TestGRPCExecutorDoesNotRedialOnUnrelatedError confirms the redial
// path is gated on the closed-client signature. A generic transport
// failure must not trigger a reconnect attempt: the orchestrator's
// other callers (Prepare, Execute, Commit) want fast-fail semantics.
func TestGRPCExecutorDoesNotRedialOnUnrelatedError(t *testing.T) {
	t.Parallel()
	otherErr := errors.New("daemon: timeout fetching meta")
	stale := &flakyClosedRPC{closeErr: otherErr}
	var redialCalls int
	exec := &GRPCExecutor{
		RPC: stale,
		Redial: func() (OPNsenseRPCClient, error) {
			redialCalls++
			return nil, nil
		},
	}
	_, err := exec.GuestExec(context.Background(), "102", "true")
	if err == nil {
		t.Fatal("expected error from non-closed failure, got nil")
	}
	if !errors.Is(err, otherErr) {
		t.Errorf("error chain = %v, want chain containing %v", err, otherErr)
	}
	if redialCalls != 0 {
		t.Errorf("redialCalls = %d, want 0 (only client-closed should redial)", redialCalls)
	}
}
