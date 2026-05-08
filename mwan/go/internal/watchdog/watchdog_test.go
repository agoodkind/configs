package watchdog

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/alert"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/notify"
	"goodkind.io/mwan/internal/ops"
)

// ---------------------------------------------------------------------------
// fakeNotifier
// ---------------------------------------------------------------------------

// notifyEvent captures one Notify or Resolve invocation for assertions.
// Resolved is true for Resolve calls and for Notify calls whose Event
// has IsRecovery set; both flow through notify.Notifier as transitions
// out of an active alert.
type notifyEvent struct {
	Kind     string
	Key      string
	Message  string
	Level    slog.Level
	Resolved bool
}

// fakeNotifier records every Notify and Resolve call so failover and
// recovery tests can assert on emitted kinds, keys, and messages
// without going through email Sink machinery.
type fakeNotifier struct {
	mu     sync.Mutex
	events []notifyEvent
	active map[string]bool
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{active: make(map[string]bool)}
}

func (f *fakeNotifier) Notify(_ context.Context, ev notify.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, notifyEvent{
		Kind:     ev.Kind,
		Key:      ev.Key,
		Message:  ev.Message,
		Level:    ev.Level,
		Resolved: ev.IsRecovery,
	})
	if !ev.IsRecovery {
		f.active[ev.Kind+"|"+ev.Key] = true
	} else {
		delete(f.active, ev.Kind+"|"+ev.Key)
	}
}

func (f *fakeNotifier) Resolve(_ context.Context, kind, key, msg string, _ ...slog.Attr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, notifyEvent{
		Kind:     kind,
		Key:      key,
		Message:  msg,
		Resolved: true,
	})
	delete(f.active, kind+"|"+key)
}

func (f *fakeNotifier) Active(kind, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active[kind+"|"+key]
}

// snapshot returns a copy of the recorded events under the lock so
// callers can iterate without holding it.
func (f *fakeNotifier) snapshot() []notifyEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]notifyEvent, len(f.events))
	copy(out, f.events)
	return out
}

// ---------------------------------------------------------------------------
// mockOps
// ---------------------------------------------------------------------------

type vmRollbackCall struct {
	VMID string
	Snap string
}

type mockOps struct {
	mu sync.Mutex

	vmRunning     bool
	vmStartErr    error
	vmStopErr     error
	vmStatusErr   error
	vmSnapErr     error
	vmRollbackErr error
	pingResults   map[string]bool
	guestResults  map[string]ops.GuestExecResult
	snapshotsOut  []byte

	// bgpStatusByVMID seeds GetBGPStatus responses keyed by the vmid argument.
	// Defaults to an empty response (not established) if the key is missing.
	bgpStatusByVMID map[string]*mwanv1.GetBGPStatusResponse

	vmStatusCalls       int
	vmStartCalls        int
	vmStopCalls         int
	vmSnapshotsCalls    int
	pingCalls           []string
	guestCalls          []string
	vmRollbackCalls     []vmRollbackCall
	announceRoutesCalls []string
	withdrawRoutesCalls []string
	getBGPStatusCalls   []string
}

func (m *mockOps) VMStatus(ctx context.Context, vmid string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vmStatusCalls++
	if m.vmStatusErr != nil {
		return false, m.vmStatusErr
	}
	return m.vmRunning, nil
}

func (m *mockOps) GetConfigState(ctx context.Context, vmid string) (*mwanv1.GetConfigStateResponse, string, error) {
	return &mwanv1.GetConfigStateResponse{}, "", nil
}

func (m *mockOps) GetBGPStatus(ctx context.Context, vmid string) (*mwanv1.GetBGPStatusResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getBGPStatusCalls = append(m.getBGPStatusCalls, vmid)
	if m.bgpStatusByVMID != nil {
		if resp, ok := m.bgpStatusByVMID[vmid]; ok {
			return resp, nil
		}
	}
	return &mwanv1.GetBGPStatusResponse{}, nil
}

func (m *mockOps) AnnounceRoutes(ctx context.Context, vmid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.announceRoutesCalls = append(m.announceRoutesCalls, vmid)
	return nil
}

func (m *mockOps) WithdrawRoutes(ctx context.Context, vmid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.withdrawRoutesCalls = append(m.withdrawRoutesCalls, vmid)
	return nil
}

func (m *mockOps) VMStart(ctx context.Context, vmid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vmStartCalls++
	return m.vmStartErr
}

func (m *mockOps) VMStop(ctx context.Context, vmid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vmStopCalls++
	return m.vmStopErr
}

func (m *mockOps) VMSnapshots(ctx context.Context, vmid string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vmSnapshotsCalls++
	if m.vmSnapErr != nil {
		return nil, m.vmSnapErr
	}
	return m.snapshotsOut, nil
}

func (m *mockOps) VMSnapshot(ctx context.Context, vmid, snapname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmSnapErr != nil {
		return m.vmSnapErr
	}
	return nil
}

func (m *mockOps) VMDelSnapshot(ctx context.Context, vmid, snapname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockOps) VMRollback(ctx context.Context, vmid, snapname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vmRollbackCalls = append(m.vmRollbackCalls, vmRollbackCall{VMID: vmid, Snap: snapname})
	return m.vmRollbackErr
}

func (m *mockOps) Ping(ctx context.Context, cmd string, target string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := cmd + ":" + target
	m.pingCalls = append(m.pingCalls, key)
	if m.pingResults != nil {
		if res, ok := m.pingResults[key]; ok {
			return res
		}
	}
	return false
}

func (m *mockOps) GuestExec(ctx context.Context, vmid string, cmd ...string) (ops.GuestExecResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.Join(append([]string{vmid}, cmd...), "|")
	m.guestCalls = append(m.guestCalls, key)
	if m.guestResults != nil {
		if res, ok := m.guestResults[key]; ok {
			return res, nil
		}
	}
	return ops.GuestExecResult{}, nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func testNC() config.NetworkConfig {
	return config.NetworkConfig{
		PingTargetIPv4: "1.1.1.1",
		PingTargetIPv6: "2606:4700:4700::1111",
		PingTargets:    []string{"2606:4700:4700::1111", "2001:4860:4860::8888"},
		CurlTarget:     "https://ifconfig.co/ip",
		WANInterfaces: []config.WANInterface{
			{Name: "enwebpass0"},
			{Name: "enmbrains0"},
		},
		LastDeployPath: "/var/lib/mwan/last-deploy",
		LastChangePath: "/var/run/mwan-last-change",
	}
}

func newTestWatchdog(
	t *testing.T, ops ops.SysOps, cfgOverrides ...func(*config.Config),
) *watchdog {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		MwanVMID: "113",
		Email: config.EmailConfig{
			AlertEmail: "test@test.com",
		},
		Network: testNC(),
		Watchdog: config.WatchdogSection{
			DeployWindowMinutes:        30,
			ConnectivityTimeoutSeconds: 0,
			CheckIntervalHealthy:       0,
			CheckIntervalDegraded:      0,
			PostRollbackGraceSeconds:   0,
			LogFile:                    filepath.Join(tmp, "watchdog.log"),
			RollbackStateFile:          filepath.Join(tmp, "rollback.state"),
			RollbackLockFile:           filepath.Join(tmp, "rollback.lock"),
			AlertCooldownSeconds:       0,
			MaxIterations:              5,
		},
	}
	for _, fn := range cfgOverrides {
		fn(cfg)
	}

	w := &watchdog{
		cfg:    cfg,
		ops:    ops,
		notify: newFakeNotifier(),
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		exitFn: os.Exit,
		coord:  &alert.Coord{},
	}
	return w
}

// fakeNotifierFrom retrieves the fake notifier wired into a test
// watchdog by newTestWatchdog. Tests that assert on emitted events
// call this rather than poking the field directly.
func fakeNotifierFrom(t *testing.T, w *watchdog) *fakeNotifier {
	t.Helper()
	fn, ok := w.notify.(*fakeNotifier)
	if !ok {
		t.Fatalf("expected *fakeNotifier on watchdog, got %T", w.notify)
	}
	return fn
}

// ---------------------------------------------------------------------------
// Test Placeholder - minimal tests to allow compilation
// ---------------------------------------------------------------------------

func TestWatchdogCompiles(t *testing.T) {
	m := &mockOps{vmRunning: true}
	w := newTestWatchdog(t, m)
	if w.cfg.MwanVMID != "113" {
		t.Fatal("watchdog not initialized correctly")
	}
}

func TestMockOpsBasics(t *testing.T) {
	m := &mockOps{
		vmRunning:   true,
		pingResults: map[string]bool{"ping:1.1.1.1": true},
	}
	running, err := m.VMStatus(context.Background(), "113")
	if !running || err != nil {
		t.Fatalf("vmstatus failed: %v", err)
	}
	if !m.Ping(context.Background(), "ping", "1.1.1.1") {
		t.Fatal("ping failed")
	}
}
