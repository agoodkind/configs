package watchdog

import (
	"context"
	"errors"
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
	"goodkind.io/mwan/internal/ops"
)

// ---------------------------------------------------------------------------
// mockOps
// ---------------------------------------------------------------------------

type vmRollbackCall struct {
	VMID string
	Snap string
}

type emailSent struct {
	To, Subject, Body string
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

	vmStatusCalls    int
	vmStartCalls     int
	vmStopCalls      int
	vmSnapshotsCalls int
	pingCalls        []string
	guestCalls       []string
	emailsSent       []emailSent
	vmRollbackCalls  []vmRollbackCall
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
	return &mwanv1.GetBGPStatusResponse{}, nil
}

func (m *mockOps) AnnounceRoutes(ctx context.Context, vmid string) error {
	return nil
}

func (m *mockOps) WithdrawRoutes(ctx context.Context, vmid string) error {
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

func (m *mockOps) SendEmail(ctx context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emailsSent = append(m.emailsSent, emailSent{To: to, Subject: subject, Body: body})
	return nil
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
		LastDeployPath: "/var/run/mwan-last-deploy",
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
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		exitFn: os.Exit,
		coord:  &alert.Coord{},
	}
	return w
}

type mockEmailSender struct {
	m *mockOps
}

func (mes *mockEmailSender) Send(ctx context.Context, to, subject, body string) error {
	return mes.m.SendEmail(ctx, to, subject, body)
}

// errSlogHandler is a test slog.Handler that returns an error.
type errSlogHandler struct{}

func (errSlogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (errSlogHandler) Handle(context.Context, slog.Record) error {
	return errors.New("handler error")
}
func (e errSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return e }
func (e errSlogHandler) WithGroup(string) slog.Handler      { return e }

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
