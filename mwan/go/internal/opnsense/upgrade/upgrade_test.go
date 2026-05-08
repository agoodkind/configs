package upgrade

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goodkind.io/mwan/internal/notify"
)

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

type recordedNotify struct {
	Kind  string
	Key   string
	Level slog.Level
	Msg   string
}

type fakeNotifier struct {
	mu     sync.Mutex
	events []recordedNotify
}

func (f *fakeNotifier) Notify(_ context.Context, ev notify.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, recordedNotify{Kind: ev.Kind, Key: ev.Key, Level: ev.Level, Msg: ev.Message})
}

func (f *fakeNotifier) Resolve(_ context.Context, kind, key, msg string, _ ...slog.Attr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, recordedNotify{Kind: kind, Key: key, Msg: msg})
}

func (f *fakeNotifier) Active(_, _ string) bool { return false }

func (f *fakeNotifier) snapshot() []recordedNotify {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedNotify, len(f.events))
	copy(out, f.events)
	return out
}

func (f *fakeNotifier) kinds() []string {
	out := make([]string, 0, len(f.events))
	for _, e := range f.snapshot() {
		out = append(out, e.Kind)
	}
	return out
}

type snapshotCall struct {
	VMID string
	Snap string
}

type fakeSnap struct {
	mu sync.Mutex

	snapErr     error
	rollbackErr error
	startErr    error
	delErr      error
	listing     []byte
	running     bool
	statusErr   error

	snapshots   []snapshotCall
	rollbacks   []snapshotCall
	deletes     []snapshotCall
	starts      []string
	statusCalls []string
}

func (s *fakeSnap) VMSnapshot(_ context.Context, vmid, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, snapshotCall{VMID: vmid, Snap: name})
	return s.snapErr
}

func (s *fakeSnap) VMRollback(_ context.Context, vmid, snap string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollbacks = append(s.rollbacks, snapshotCall{VMID: vmid, Snap: snap})
	return s.rollbackErr
}

func (s *fakeSnap) VMSnapshots(_ context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listing, nil
}

func (s *fakeSnap) VMDelSnapshot(_ context.Context, vmid, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes = append(s.deletes, snapshotCall{VMID: vmid, Snap: name})
	return s.delErr
}

func (s *fakeSnap) VMStart(_ context.Context, vmid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts = append(s.starts, vmid)
	return s.startErr
}

func (s *fakeSnap) VMStatus(_ context.Context, vmid string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCalls = append(s.statusCalls, vmid)
	return s.running, s.statusErr
}

type execCall struct {
	VMID string
	Args []string
}

type fakeExec struct {
	mu sync.Mutex

	calls   []execCall
	results []GuestExecResult
	errs    []error
	idx     int
	delay   time.Duration
}

func (e *fakeExec) GuestExec(ctx context.Context, vmid string, args ...string) (GuestExecResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, execCall{VMID: vmid, Args: append([]string(nil), args...)})
	i := e.idx
	if i >= len(e.results) {
		i = len(e.results) - 1
	}
	res := GuestExecResult{}
	if i >= 0 && i < len(e.results) {
		res = e.results[i]
	}
	var err error
	if i >= 0 && i < len(e.errs) {
		err = e.errs[i]
	}
	e.idx++
	delay := e.delay
	e.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return GuestExecResult{}, ctx.Err()
		}
	}
	return res, err
}

type fakeValidator struct {
	result ValidationResult
	err    error
	calls  int
}

func (v *fakeValidator) Validate(_ context.Context, _ ValidateContext) (ValidationResult, error) {
	v.calls++
	return v.result, v.err
}

type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time { return c.t }

func newDeps(t *testing.T) (Deps, *fakeNotifier, *fakeSnap, *fakeExec, *fakeValidator) {
	t.Helper()
	n := &fakeNotifier{}
	s := &fakeSnap{}
	x := &fakeExec{results: []GuestExecResult{{ExitCode: 0}}}
	v := &fakeValidator{result: AggregateChecks([]CheckResult{{Name: "qga_responsive", Pass: true}})}
	deps := Deps{
		Snap:     s,
		Exec:     x,
		Validate: v,
		Notifier: n,
		Clock:    fixedClock{t: time.Unix(1_700_000_000, 0)},
		Log:      slog.New(slog.NewTextHandler(testWriter{t: t}, nil)),
	}
	return deps, n, s, x, v
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Logf("%s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func newOpts(t *testing.T, vmid string) Options {
	t.Helper()
	dir := t.TempDir()
	return Options{
		VMID:                vmid,
		Target:              "26.7",
		StateDir:            dir,
		UpgradeTimeout:      5 * time.Second,
		PostRollbackTimeout: 1 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// state machine tests
// ---------------------------------------------------------------------------

func TestCanTransitionAllowsDocumentedEdges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from Phase
		to   Phase
		want bool
	}{
		{PhaseEmpty, PhasePrepared, true},
		{PhasePrepared, PhaseExecuting, true},
		{PhaseExecuting, PhaseExecuted, true},
		{PhaseExecuted, PhaseValidatedPass, true},
		{PhaseValidatedPass, PhaseCommitted, true},
		{PhaseValidatedFail, PhaseRolledBack, true},
		{PhaseRolledBack, PhaseCommitted, true},
		{PhaseValidatedPass, PhaseRolledBack, false},
		{PhaseCommitted, PhasePrepared, false},
		{PhaseRollbackFailed, PhaseRolledBack, false},
	}
	for _, tc := range cases {
		got := CanTransition(tc.from, tc.to)
		if got != tc.want {
			t.Errorf("CanTransition(%s -> %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestEnforceTransitionReturnsTypedError(t *testing.T) {
	t.Parallel()
	err := EnforceTransition(PhaseCommitted, PhasePrepared)
	var typed TransitionNotAllowedError
	if !errors.As(err, &typed) {
		t.Fatalf("expected TransitionNotAllowedError, got %v", err)
	}
	if typed.From != PhaseCommitted || typed.To != PhasePrepared {
		t.Fatalf("error fields = %+v", typed)
	}
}

func TestSnapshotNameAndIsUpgradeSnapshot(t *testing.T) {
	t.Parallel()
	name := SnapshotName(time.Unix(1_700_000_000, 0))
	if !strings.HasPrefix(name, SnapshotPrefix) {
		t.Fatalf("snapshot name %q missing prefix", name)
	}
	if !IsUpgradeSnapshot(name) {
		t.Fatalf("IsUpgradeSnapshot(%q) = false", name)
	}
	if IsUpgradeSnapshot("pre-deploy-1700000000") {
		t.Fatalf("watchdog snapshot accepted as upgrade snapshot")
	}
	if IsUpgradeSnapshot(KeepPrefix + "1700000000") {
		t.Fatalf("kept snapshot must not match IsUpgradeSnapshot")
	}
}

func TestStateRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := State{
		VMID: "101", DeployID: "abc", Target: "26.7", Snapshot: "pre-upgrade-26x-1",
		Phase: PhasePrepared, UpdatedAt: time.Time{}, FailingCheck: nil,
	}
	if err := saveStateCtx(context.Background(), dir, st, time.Unix(1, 0)); err != nil {
		t.Fatalf("saveStateCtx: %v", err)
	}
	loaded, err := loadStateCtx(context.Background(), dir, "101")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.Phase != PhasePrepared || loaded.DeployID != "abc" {
		t.Fatalf("loaded = %+v", loaded)
	}
	missing, err := loadStateCtx(context.Background(), dir, "999")
	if err != nil {
		t.Fatalf("LoadState missing: %v", err)
	}
	if missing.Phase != PhaseEmpty {
		t.Fatalf("missing state phase = %q, want empty", missing.Phase)
	}
}

// ---------------------------------------------------------------------------
// prepare
// ---------------------------------------------------------------------------

func TestPrepareTakesSnapshotAndWritesState(t *testing.T) {
	t.Parallel()
	deps, n, s, _, _ := newDeps(t)
	opts := newOpts(t, "101")

	st, err := Prepare(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if st.Phase != PhasePrepared {
		t.Fatalf("phase = %q, want %q", st.Phase, PhasePrepared)
	}
	if len(s.snapshots) != 1 {
		t.Fatalf("snapshot calls = %d, want 1", len(s.snapshots))
	}
	if !strings.HasPrefix(s.snapshots[0].Snap, SnapshotPrefix) {
		t.Fatalf("snapshot name = %q", s.snapshots[0].Snap)
	}
	if !ContainsKind(n.kinds(), KindPrepare) {
		t.Fatalf("expected prepare kind, got %v", n.kinds())
	}
	if st.DeployID == "" {
		t.Fatalf("DeployID empty")
	}
	if _, statErr := readJSON(filepath.Join(opts.StateDir, "101", st.DeployID, "metadata.json")); statErr != nil {
		t.Fatalf("metadata.json missing: %v", statErr)
	}
}

func TestPrepareSnapshotFailureDoesNotWriteState(t *testing.T) {
	t.Parallel()
	deps, n, s, _, _ := newDeps(t)
	opts := newOpts(t, "101")
	s.snapErr = errors.New("snapshot exploded")

	st, err := Prepare(context.Background(), deps, opts)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if st.Phase == PhasePrepared {
		t.Fatalf("phase should not be prepared after snapshot error")
	}
	loaded, _ := loadStateCtx(context.Background(), opts.StateDir, "101")
	if loaded.Phase == PhasePrepared {
		t.Fatalf("state file must not be left at prepared after snapshot error")
	}
	if !ContainsKind(n.kinds(), KindPrepare) {
		t.Fatalf("expected prepare error notify, got %v", n.kinds())
	}
}

// ---------------------------------------------------------------------------
// execute
// ---------------------------------------------------------------------------

func TestExecuteHappyPathReachesExecuted(t *testing.T) {
	t.Parallel()
	deps, _, _, _, _ := newDeps(t)
	opts := newOpts(t, "101")
	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	st, err := Execute(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if st.Phase != PhaseExecuted {
		t.Fatalf("phase = %q, want executed", st.Phase)
	}
}

func TestExecuteNonZeroExitTransitionsToExecuteFailed(t *testing.T) {
	t.Parallel()
	deps, _, _, x, _ := newDeps(t)
	x.results = []GuestExecResult{{ExitCode: 1, Stderr: "boom"}}
	opts := newOpts(t, "101")
	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	st, err := Execute(context.Background(), deps, opts)
	if err == nil {
		t.Fatalf("expected error")
	}
	if st.Phase != PhaseExecuteFailed {
		t.Fatalf("phase = %q, want execute_failed", st.Phase)
	}
}

// ---------------------------------------------------------------------------
// validate
// ---------------------------------------------------------------------------

func TestValidatePassRecordsValidatedPass(t *testing.T) {
	t.Parallel()
	deps, _, _, _, _ := newDeps(t)
	opts := newOpts(t, "101")
	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := Execute(context.Background(), deps, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}
	st, res, err := Validate(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !res.AllPass {
		t.Fatalf("AllPass = false")
	}
	if st.Phase != PhaseValidatedPass {
		t.Fatalf("phase = %q", st.Phase)
	}
}

func TestValidateFailRecordsValidatedFail(t *testing.T) {
	t.Parallel()
	deps, _, _, _, v := newDeps(t)
	v.result = AggregateChecks([]CheckResult{{Name: "qga_responsive", Pass: false}})
	opts := newOpts(t, "101")
	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := Execute(context.Background(), deps, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}
	st, _, err := Validate(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if st.Phase != PhaseValidatedFail {
		t.Fatalf("phase = %q", st.Phase)
	}
	if len(st.FailingCheck) == 0 {
		t.Fatalf("expected failing check names recorded")
	}
}

func TestValidatePartialAcceptedTransitionsToPartial(t *testing.T) {
	t.Parallel()
	deps, _, _, _, v := newDeps(t)
	v.result = AggregateChecks([]CheckResult{
		{Name: "qga_responsive", Pass: true},
		{Name: "frr_state", Pass: false},
	})
	opts := newOpts(t, "101")
	opts.AcceptPartial = true
	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := Execute(context.Background(), deps, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}
	st, _, err := Validate(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if st.Phase != PhaseValidatedPartial {
		t.Fatalf("phase = %q, want validated_partial", st.Phase)
	}
}

// ---------------------------------------------------------------------------
// rollback
// ---------------------------------------------------------------------------

func TestRollbackOnValidateFailRestoresSnapshot(t *testing.T) {
	t.Parallel()
	deps, _, s, x, v := newDeps(t)
	v.result = AggregateChecks([]CheckResult{{Name: "qga_responsive", Pass: false}})
	x.results = []GuestExecResult{{ExitCode: 0}}
	s.running = true
	opts := newOpts(t, "101")

	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := Execute(context.Background(), deps, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, _, err := Validate(context.Background(), deps, opts); err != nil {
		t.Fatalf("validate: %v", err)
	}

	st, err := Rollback(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if st.Phase != PhaseRolledBack {
		t.Fatalf("phase = %q", st.Phase)
	}
	if len(s.rollbacks) != 1 {
		t.Fatalf("rollback calls = %d", len(s.rollbacks))
	}
}

func TestRollbackFailureMarksRollbackFailed(t *testing.T) {
	t.Parallel()
	deps, n, s, _, v := newDeps(t)
	v.result = AggregateChecks([]CheckResult{{Name: "qga_responsive", Pass: false}})
	s.rollbackErr = errors.New("rollback exploded")
	opts := newOpts(t, "101")

	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := Execute(context.Background(), deps, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, _, err := Validate(context.Background(), deps, opts); err != nil {
		t.Fatalf("validate: %v", err)
	}

	st, err := Rollback(context.Background(), deps, opts)
	if err == nil {
		t.Fatalf("expected error from failed rollback")
	}
	if st.Phase != PhaseRollbackFailed {
		t.Fatalf("phase = %q, want rollback_failed", st.Phase)
	}
	if !ContainsKind(n.kinds(), KindRollbackFailed) {
		t.Fatalf("expected rollback-failed notify, got %v", n.kinds())
	}
}

// ---------------------------------------------------------------------------
// commit
// ---------------------------------------------------------------------------

func TestCommitDeletesSnapshotAfterValidatedPass(t *testing.T) {
	t.Parallel()
	deps, _, s, _, _ := newDeps(t)
	opts := newOpts(t, "101")

	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := Execute(context.Background(), deps, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, _, err := Validate(context.Background(), deps, opts); err != nil {
		t.Fatalf("validate: %v", err)
	}

	st, err := Commit(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if st.Phase != PhaseCommitted {
		t.Fatalf("phase = %q", st.Phase)
	}
	if len(s.deletes) == 0 {
		t.Fatalf("expected snapshot delete on commit")
	}
}

func TestCommitFromBadPhaseRefuses(t *testing.T) {
	t.Parallel()
	deps, _, _, _, _ := newDeps(t)
	opts := newOpts(t, "101")
	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	_, err := Commit(context.Background(), deps, opts)
	var typed TransitionNotAllowedError
	if !errors.As(err, &typed) {
		t.Fatalf("expected transition not allowed, got %v", err)
	}
}

func TestCommitIdempotent(t *testing.T) {
	t.Parallel()
	deps, _, _, _, _ := newDeps(t)
	opts := newOpts(t, "101")

	if _, err := Prepare(context.Background(), deps, opts); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := Execute(context.Background(), deps, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, _, err := Validate(context.Background(), deps, opts); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if _, err := Commit(context.Background(), deps, opts); err != nil {
		t.Fatalf("commit: %v", err)
	}
	st, err := Commit(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("commit second: %v", err)
	}
	if st.Phase != PhaseCommitted {
		t.Fatalf("phase = %q", st.Phase)
	}
}

// ---------------------------------------------------------------------------
// run
// ---------------------------------------------------------------------------

func TestRunHappyPathReachesValidatedPass(t *testing.T) {
	t.Parallel()
	deps, _, _, _, _ := newDeps(t)
	opts := newOpts(t, "101")
	out, err := Run(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reached != PhaseValidatedPass {
		t.Fatalf("reached = %q, want validated_pass", out.Reached)
	}
	if out.AutoRollback {
		t.Fatalf("auto rollback fired on happy path")
	}
}

func TestRunValidateFailAutoRolls(t *testing.T) {
	t.Parallel()
	deps, _, s, _, v := newDeps(t)
	v.result = AggregateChecks([]CheckResult{{Name: "frr_state", Pass: false}})
	s.running = true
	opts := newOpts(t, "101")

	out, err := Run(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.AutoRollback {
		t.Fatalf("auto rollback did not fire on validate fail")
	}
	if out.Reached != PhaseRolledBack {
		t.Fatalf("reached = %q, want rolled_back", out.Reached)
	}
}

// ---------------------------------------------------------------------------
// gc
// ---------------------------------------------------------------------------

func TestGCDeletesOldSnapshotsAndKeepsYoung(t *testing.T) {
	t.Parallel()
	deps, _, s, _, _ := newDeps(t)
	now := time.Unix(1_800_000_000, 0)
	deps.Clock = fixedClock{t: now}
	old := SnapshotName(now.Add(-30 * 24 * time.Hour))
	young := SnapshotName(now.Add(-1 * time.Hour))
	listing := []byte(" `-> " + old + " 2026-04-01 desc\n `-> " + young + " 2026-04-08 desc\n")
	s.listing = listing
	opts := newOpts(t, "101")

	res, err := GC(context.Background(), deps, opts)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(res.Deleted) != 1 || res.Deleted[0] != old {
		t.Fatalf("deleted = %v, want [%s]", res.Deleted, old)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != young {
		t.Fatalf("skipped = %v, want [%s]", res.Skipped, young)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func readJSON(path string) ([]byte, error) {
	// Path is constructed from t.TempDir output plus the operator-set
	// VMID and DeployID, so gosec G304 does not apply here.
	return os.ReadFile(path) //nolint:gosec
}

// ContainsKind reports whether any of the given notify kinds match
// the wanted kind, case-insensitively. Lives in the test file because
// only the unit tests use it; production code does not introspect the
// emitted kind list.
func ContainsKind(kinds []string, want string) bool {
	for _, k := range kinds {
		if strings.EqualFold(k, want) {
			return true
		}
	}
	return false
}
