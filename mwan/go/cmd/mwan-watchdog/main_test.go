package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mwanv1 "github.com/agoodkind/infra-tools/gen/mwan/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
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

	vmRunning    bool
	vmStatusErr  error
	snapshotsOut []byte
	guestResults map[string]guestExecResult
	guestErr     error
	pingResults  map[string]bool

	vmStopCalls     []string
	vmRollbackCalls []vmRollbackCall
	vmStartCalls    []string
	emailsSent      []emailSent

	pingCallCount      int
	guestExecCallCount int

	vmStopErr      error
	vmRollbackErr  error
	vmStartErr     error
	vmSnapshotsErr error
	sendEmailErr   error

	vmStatusFn func(context.Context, string) (bool, error)

	vmSnapshotCalls   []vmSnapshotCall
	vmDelSnapshotCalls []vmDelSnapshotCall
	vmSnapshotErr     error
	vmDelSnapshotErr  error
}

type vmSnapshotCall struct {
	VMID string
	Name string
}

type vmDelSnapshotCall struct {
	VMID string
	Name string
}

func (m *mockOps) vmStatus(ctx context.Context, vmid string) (bool, error) {
	m.mu.Lock()
	fn := m.vmStatusFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, vmid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmStatusErr != nil {
		return false, m.vmStatusErr
	}
	return m.vmRunning, nil
}

func (m *mockOps) vmStop(_ context.Context, vmid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmStopErr != nil {
		return m.vmStopErr
	}
	m.vmStopCalls = append(m.vmStopCalls, vmid)
	return nil
}

func (m *mockOps) vmRollback(_ context.Context, vmid, snap string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmRollbackErr != nil {
		return m.vmRollbackErr
	}
	m.vmRollbackCalls = append(m.vmRollbackCalls, vmRollbackCall{VMID: vmid, Snap: snap})
	return nil
}

func (m *mockOps) vmStart(_ context.Context, vmid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmStartErr != nil {
		return m.vmStartErr
	}
	m.vmStartCalls = append(m.vmStartCalls, vmid)
	return nil
}

func (m *mockOps) vmSnapshots(_ context.Context, _ string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmSnapshotsErr != nil {
		return nil, m.vmSnapshotsErr
	}
	return m.snapshotsOut, nil
}

func (m *mockOps) vmSnapshot(_ context.Context, vmid, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmSnapshotErr != nil {
		return m.vmSnapshotErr
	}
	m.vmSnapshotCalls = append(m.vmSnapshotCalls, vmSnapshotCall{VMID: vmid, Name: name})
	return nil
}

func (m *mockOps) vmDelSnapshot(_ context.Context, vmid, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmDelSnapshotErr != nil {
		return m.vmDelSnapshotErr
	}
	m.vmDelSnapshotCalls = append(m.vmDelSnapshotCalls, vmDelSnapshotCall{VMID: vmid, Name: name})
	return nil
}

func (m *mockOps) guestExec(
	_ context.Context, _ string, args ...string,
) (guestExecResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guestExecCallCount++
	if m.guestErr != nil {
		return guestExecResult{ExitCode: 1}, m.guestErr
	}
	key := strings.Join(args, " ")
	if m.guestResults == nil {
		return guestExecResult{ExitCode: 1}, nil
	}
	if r, ok := m.guestResults[key]; ok {
		return r, nil
	}
	return guestExecResult{ExitCode: 1}, nil
}

func (m *mockOps) ping(_ context.Context, bin, target string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingCallCount++
	key := bin + ":" + target
	if m.pingResults == nil {
		return false
	}
	if v, ok := m.pingResults[key]; ok {
		return v
	}
	return false
}

func (m *mockOps) sendEmail(
	_ context.Context, to, subject, body string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendEmailErr != nil {
		return m.sendEmailErr
	}
	m.emailsSent = append(m.emailsSent, emailSent{
		To: to, Subject: subject, Body: body,
	})
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testNC returns the default network config; tests may mutate it.
func testNC() networkConfig {
	return defaultNetworkConfig()
}

func newTestWatchdog(
	t *testing.T, ops sysOps, cfgOverrides ...func(*config),
) *watchdog {
	t.Helper()
	tmp := t.TempDir()
	cfg := config{
		MwanVMID:                   "113",
		DeployWindowMinutes:        30,
		ConnectivityTimeoutSeconds: 0,
		CheckIntervalHealthy:       0,
		CheckIntervalDegraded:      0,
		PostRollbackGraceSeconds:   0,
		LogFile:                    filepath.Join(tmp, "watchdog.log"),
		RollbackStateFile:          filepath.Join(tmp, "rollback.state"),
		RollbackLockFile:           filepath.Join(tmp, "rollback.lock"),
		AlertEmail:                 "test@test.com",
		AlertCooldownSeconds:       0,
		MaxIterations:              5,
	}
	for _, fn := range cfgOverrides {
		fn(&cfg)
	}
	testLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &watchdog{
		cfg:     cfg,
		nc:      testNC(),
		ops:     ops,
		coord:   &watchdogCoord{},
		limiter: newAlertLimiter(cfg.AlertCooldownSeconds),
		log:     testLog,
	}
}

func runWatchdogUntilDone(t *testing.T, w *watchdog) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Log("watchdog run did not finish after cancel")
		}
	})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
}

// ---------------------------------------------------------------------------
// guest key helpers: build expected guestExec arg strings from network config
// ---------------------------------------------------------------------------

func guestKeyPing6Default(nc networkConfig) string {
	return strings.Join(
		[]string{"ping6", "-c", "2", "-W", "3", nc.PingTargetIPv6}, " ",
	)
}

func guestKeyPingDefault(nc networkConfig) string {
	return strings.Join(
		[]string{"ping", "-c", "2", "-W", "3", nc.PingTargetIPv4}, " ",
	)
}

// guestKeyISP builds the first WAN interface IPv4 ping key.
func guestKeyISP(nc networkConfig) string {
	iface := nc.WANInterfaces[0].Name
	return strings.Join(
		[]string{"ping", "-c", "3", "-W", "3", "-I", iface, nc.PingTargetIPv4}, " ",
	)
}

func guestKeyDeployCat(nc networkConfig) string {
	return strings.Join([]string{"cat", nc.LastDeployPath}, " ")
}

func guestKeyChangeCat(nc networkConfig) string {
	return strings.Join([]string{"cat", nc.LastChangePath}, " ")
}

func guestKeyConfigHashCat(nc networkConfig) string {
	return strings.Join([]string{"cat", nc.ConfigHashPath}, " ")
}

// allISPKeys returns all WAN interface ping keys used in total-loss scenarios.
func allISPKeys(nc networkConfig) []string {
	var keys []string
	for _, w := range nc.WANInterfaces {
		keys = append(keys,
			strings.Join([]string{"ping", "-c", "3", "-W", "3", "-I", w.Name, nc.PingTargetIPv4}, " "),
			strings.Join([]string{"ping6", "-c", "3", "-W", "3", "-I", w.Name, nc.PingTargetIPv6}, " "),
		)
	}
	return keys
}

// ---------------------------------------------------------------------------
// Pure function tests
// ---------------------------------------------------------------------------

func TestExtractLatestSnapshot(t *testing.T) {
	t.Parallel()
	t.Run("multiple pick last", func(t *testing.T) {
		out := []byte("foo\n`-> pre-deploy-a\nbar\n`-> pre-deploy-b\n")
		if got := extractLatestSnapshot(out); got != "pre-deploy-b" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("no matches", func(t *testing.T) {
		if got := extractLatestSnapshot([]byte("nothing here")); got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("single match", func(t *testing.T) {
		out := []byte("`-> pre-deploy-only\n")
		if got := extractLatestSnapshot(out); got != "pre-deploy-only" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("known-good fallback", func(t *testing.T) {
		out := []byte("x\n`-> known-good-a\n")
		if got := extractLatestSnapshot(out); got != "known-good-a" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("pre-deploy preferred over known-good", func(t *testing.T) {
		out := []byte("`-> known-good-z\n`-> pre-deploy-a\n")
		if got := extractLatestSnapshot(out); got != "pre-deploy-a" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestParseRollbackStateFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "state")

	t.Run("well-formed", func(t *testing.T) {
		content := "deploy_timestamp=42\nrollback_done=true\nsnapshot=snap1\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		ds, done, snap, err := parseRollbackStateFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if ds != "42" || !done || snap != "snap1" {
			t.Fatalf("got %q %v %q", ds, done, snap)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		content := "other=1\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		ds, done, snap, err := parseRollbackStateFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if ds != "" || done || snap != "" {
			t.Fatalf("got %q %v %q", ds, done, snap)
		}
	})

	t.Run("comments and blank lines", func(t *testing.T) {
		content := "# comment\n\ndeploy_timestamp=7\n\n# x\nrollback_done=false\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		ds, done, snap, err := parseRollbackStateFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if ds != "7" || done || snap != "" {
			t.Fatalf("got %q %v %q", ds, done, snap)
		}
	})
}

func TestWriteRollbackState(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "rb.state")
	deployTS := int64(12345)
	snap := "pre-deploy-test"
	if err := writeRollbackState(path, deployTS, snap); err != nil {
		t.Fatal(err)
	}
	ds, done, readSnap, err := parseRollbackStateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if ds != strconv.FormatInt(deployTS, 10) || !done || readSnap != snap {
		t.Fatalf("got %q %v %q", ds, done, readSnap)
	}
}

func TestRollbackAlreadyDone(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "state")

	t.Run("matching deploy_ts done true", func(t *testing.T) {
		_ = os.WriteFile(
			path,
			[]byte("deploy_timestamp=99\nrollback_done=true\n"),
			0o644,
		)
		ok, err := rollbackAlreadyDone(path, 99)
		if err != nil || !ok {
			t.Fatalf("got %v %v", ok, err)
		}
	})

	t.Run("mismatched deploy_ts", func(t *testing.T) {
		_ = os.WriteFile(
			path,
			[]byte("deploy_timestamp=99\nrollback_done=true\n"),
			0o644,
		)
		ok, err := rollbackAlreadyDone(path, 100)
		if err != nil || ok {
			t.Fatalf("got %v %v", ok, err)
		}
	})

	t.Run("missing file returns false", func(t *testing.T) {
		missing := filepath.Join(tmp, "nope")
		ok, err := rollbackAlreadyDone(missing, 1)
		if err != nil || ok {
			t.Fatalf("got %v %v", ok, err)
		}
	})
}

func TestAlertLimiter(t *testing.T) {
	t.Parallel()
	cooldown := 300
	l := newAlertLimiter(cooldown)
	t0 := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	tWithin := t0.Add(1 * time.Second)
	tAfter := t0.Add(time.Duration(cooldown+1) * time.Second)

	if !l.trySendPartial(t0) {
		t.Fatal("first partial should be true")
	}
	if l.trySendPartial(tWithin) {
		t.Fatal("second partial within cooldown should be false")
	}
	if !l.trySendPartial(tAfter) {
		t.Fatal("after cooldown partial should be true")
	}

	l2 := newAlertLimiter(cooldown)
	if !l2.trySendTotal(t0) {
		t.Fatal("first total should be true")
	}
	if l2.trySendTotal(tWithin) {
		t.Fatal("second total within cooldown should be false")
	}
	l2.resetCooldowns()
	if !l2.trySendPartial(t0) {
		t.Fatal("after reset partial should allow")
	}
	if !l2.trySendTotal(t0) {
		t.Fatal("after reset total should allow")
	}
}

func TestLoadConfig(t *testing.T) {
	keys := []string{
		"MWAN_VMID",
		"DEPLOY_WINDOW_MINUTES",
		"CONNECTIVITY_TIMEOUT_SECONDS",
		"CHECK_INTERVAL_HEALTHY",
		"CHECK_INTERVAL_DEGRADED",
		"POST_ROLLBACK_GRACE_SECONDS",
		"LOG_FILE",
		"LOG_JSON_FILE",
		"ROLLBACK_STATE_FILE",
		"ROLLBACK_LOCK_FILE",
		"ALERT_EMAIL",
		"ALERT_COOLDOWN_SECONDS",
		"SMTP2GO_API_KEY",
	}
	clearEnv := func(t *testing.T) {
		t.Helper()
		for _, k := range keys {
			t.Setenv(k, "")
		}
	}

	t.Run("defaults when env empty", func(t *testing.T) {
		clearEnv(t)
		cfg, err := loadConfig(false)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MwanVMID != "113" {
			t.Fatalf("MwanVMID %q", cfg.MwanVMID)
		}
		if cfg.DeployWindowMinutes != 30 {
			t.Fatalf("DeployWindowMinutes %d", cfg.DeployWindowMinutes)
		}
		if cfg.ConnectivityTimeoutSeconds != 30 {
			t.Fatalf("ConnectivityTimeoutSeconds %d", cfg.ConnectivityTimeoutSeconds)
		}
		if cfg.CheckIntervalHealthy != 30*time.Second {
			t.Fatalf("CheckIntervalHealthy %v", cfg.CheckIntervalHealthy)
		}
		if cfg.AlertEmail != "root@localhost" {
			t.Fatalf("AlertEmail %q", cfg.AlertEmail)
		}
	})

	t.Run("custom values via env", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("MWAN_VMID", "200")
		t.Setenv("DEPLOY_WINDOW_MINUTES", "15")
		t.Setenv("CONNECTIVITY_TIMEOUT_SECONDS", "45")
		t.Setenv("CHECK_INTERVAL_HEALTHY", "5")
		t.Setenv("ALERT_EMAIL", "ops@example.com")
		cfg, err := loadConfig(false)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MwanVMID != "200" ||
			cfg.DeployWindowMinutes != 15 ||
			cfg.ConnectivityTimeoutSeconds != 45 ||
			cfg.CheckIntervalHealthy != 5*time.Second ||
			cfg.AlertEmail != "ops@example.com" {
			t.Fatalf("cfg %+v", cfg)
		}
	})

	t.Run("missing SMTP2GO_API_KEY requireAPIKey true errors", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("SMTP2GO_API_KEY", "")
		_, err := loadConfig(true)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing key requireAPIKey false succeeds", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("SMTP2GO_API_KEY", "")
		cfg, err := loadConfig(false)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SMTP2GOAPIKey != "" {
			t.Fatalf("want empty key, got %q", cfg.SMTP2GOAPIKey)
		}
	})
}

func TestLoadNetworkConfig(t *testing.T) {
	t.Parallel()

	t.Run("defaults when file absent", func(t *testing.T) {
		nc, err := loadNetworkConfig("/tmp/does-not-exist-mwan-network.toml")
		if err != nil {
			t.Fatal(err)
		}
		if nc.PingTargetIPv4 != "1.1.1.1" {
			t.Fatalf("PingTargetIPv4 %q", nc.PingTargetIPv4)
		}
		if len(nc.WANInterfaces) == 0 {
			t.Fatal("expected default WAN interfaces")
		}
	})

	t.Run("empty path uses defaults", func(t *testing.T) {
		nc, err := loadNetworkConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if len(nc.WANInterfaces) == 0 {
			t.Fatal("expected default WAN interfaces")
		}
	})

	t.Run("override via TOML", func(t *testing.T) {
		tmp := t.TempDir()
		f := tmp + "/network.toml"
		content := `
ping_target_ipv4 = "8.8.8.8"
ping_target_ipv6 = "2001:4860:4860::8888"

[[wan_interfaces]]
name = "enp1s0"

[[wan_interfaces]]
name = "enp2s0"
`
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		nc, err := loadNetworkConfig(f)
		if err != nil {
			t.Fatal(err)
		}
		if nc.PingTargetIPv4 != "8.8.8.8" {
			t.Fatalf("PingTargetIPv4 %q", nc.PingTargetIPv4)
		}
		if len(nc.WANInterfaces) != 2 ||
			nc.WANInterfaces[0].Name != "enp1s0" ||
			nc.WANInterfaces[1].Name != "enp2s0" {
			t.Fatalf("WANInterfaces %+v", nc.WANInterfaces)
		}
	})

	t.Run("empty wan_interfaces rejected", func(t *testing.T) {
		tmp := t.TempDir()
		f := tmp + "/network.toml"
		content := "ping_target_ipv4 = \"1.1.1.1\"\nwan_interfaces = []\n"
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := loadNetworkConfig(f)
		if err == nil {
			t.Fatal("expected error for empty wan_interfaces")
		}
	})
}

// ---------------------------------------------------------------------------
// Watchdog scenario tests
// ---------------------------------------------------------------------------

func TestWatchdog_Healthy(t *testing.T) {
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  true,
			"ping6:" + nc.PingTargetIPv6: true,
		},
	}
	w := newTestWatchdog(t, m)
	w.run(context.Background())
	if w.lastState != stateHealthy {
		t.Fatalf("lastState %q", w.lastState)
	}
	if len(m.emailsSent) != 0 ||
		len(m.vmStopCalls) != 0 ||
		len(m.vmRollbackCalls) != 0 ||
		len(m.vmStartCalls) != 0 {
		t.Fatalf("unexpected side effects: emails=%d stop=%d rb=%d start=%d",
			len(m.emailsSent), len(m.vmStopCalls),
			len(m.vmRollbackCalls), len(m.vmStartCalls))
	}
}

func TestWatchdog_PartialIPv4Loss(t *testing.T) {
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  false,
			"ping6:" + nc.PingTargetIPv6: true,
		},
	}
	w := newTestWatchdog(t, m)
	w.run(context.Background())
	if w.lastState != statePartial {
		t.Fatalf("lastState %q", w.lastState)
	}
	if !emailSubjectContains(m.emailsSent, "IPv4") {
		t.Fatalf("emails: %+v", m.emailsSent)
	}
	if len(m.vmStopCalls) != 0 || len(m.vmRollbackCalls) != 0 {
		t.Fatal("rollback should not run")
	}
}

func TestWatchdog_PartialIPv6Loss(t *testing.T) {
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  true,
			"ping6:" + nc.PingTargetIPv6: false,
		},
	}
	w := newTestWatchdog(t, m)
	w.run(context.Background())
	if w.lastState != statePartial {
		t.Fatalf("lastState %q", w.lastState)
	}
	if !emailSubjectContains(m.emailsSent, "IPv6") {
		t.Fatalf("emails: %+v", m.emailsSent)
	}
}

func TestWatchdog_TotalLoss_MWANFailure_Rollback(t *testing.T) {
	nc := testNC()
	recentTS := strconv.FormatInt(time.Now().Unix()-60, 10)
	fail := guestExecResult{ExitCode: 1}
	results := map[string]guestExecResult{
		guestKeyPing6Default(nc): fail,
		guestKeyPingDefault(nc):  fail,
		guestKeyDeployCat(nc):    {ExitCode: 0, Stdout: recentTS},
	}
	// first WAN interface IPv4 succeeds -> ISP is up
	results[guestKeyISP(nc)] = guestExecResult{ExitCode: 0}
	m := &mockOps{
		vmRunning:    true,
		pingResults:  map[string]bool{"ping:" + nc.PingTargetIPv4: false, "ping6:" + nc.PingTargetIPv6: false},
		guestResults: results,
		snapshotsOut: []byte("`-> pre-deploy-rollback-test\n"),
	}
	w := newTestWatchdog(t, m, func(c *config) { c.MaxIterations = 2 })
	runWatchdogUntilDone(t, w)

	if len(m.vmStopCalls) != 1 || m.vmStopCalls[0] != "113" {
		t.Fatalf("vmStopCalls %v", m.vmStopCalls)
	}
	if len(m.vmRollbackCalls) != 1 ||
		m.vmRollbackCalls[0].VMID != "113" ||
		m.vmRollbackCalls[0].Snap != "pre-deploy-rollback-test" {
		t.Fatalf("vmRollbackCalls %+v", m.vmRollbackCalls)
	}
	if len(m.vmStartCalls) != 1 || m.vmStartCalls[0] != "113" {
		t.Fatalf("vmStartCalls %v", m.vmStartCalls)
	}
	if !emailSubjectContains(m.emailsSent, "AUTO-ROLLBACK") {
		t.Fatalf("rollback email missing: %+v", m.emailsSent)
	}
	ds, done, snap, err := parseRollbackStateFile(w.cfg.RollbackStateFile)
	if err != nil {
		t.Fatal(err)
	}
	if ds != recentTS || !done || snap != "pre-deploy-rollback-test" {
		t.Fatalf("state file %q %v %q", ds, done, snap)
	}
}

func TestWatchdog_TotalLoss_ISPOutage_NoRollback(t *testing.T) {
	nc := testNC()
	recentTS := strconv.FormatInt(time.Now().Unix()-60, 10)
	fail := guestExecResult{ExitCode: 1}
	results := map[string]guestExecResult{
		guestKeyPing6Default(nc): fail,
		guestKeyPingDefault(nc):  fail,
		guestKeyDeployCat(nc):    {ExitCode: 0, Stdout: recentTS},
	}
	for _, k := range allISPKeys(nc) {
		results[k] = fail
	}
	m := &mockOps{
		vmRunning:    true,
		pingResults:  map[string]bool{"ping:" + nc.PingTargetIPv4: false, "ping6:" + nc.PingTargetIPv6: false},
		guestResults: results,
		snapshotsOut: []byte("`-> pre-deploy-x\n"),
	}
	w := newTestWatchdog(t, m, func(c *config) { c.MaxIterations = 2 })
	runWatchdogUntilDone(t, w)
	if len(m.vmStopCalls) != 0 {
		t.Fatalf("expected no rollback, vmStop=%v", m.vmStopCalls)
	}
	for _, e := range m.emailsSent {
		if strings.Contains(e.Subject, "AUTO-ROLLBACK") {
			t.Fatalf("unexpected rollback email: %+v", e)
		}
	}
}

func TestWatchdog_TotalLoss_ProxmoxRouting(t *testing.T) {
	nc := testNC()
	m := &mockOps{
		vmRunning:   true,
		pingResults: map[string]bool{"ping:" + nc.PingTargetIPv4: false, "ping6:" + nc.PingTargetIPv6: false},
		guestResults: map[string]guestExecResult{
			guestKeyPing6Default(nc): {ExitCode: 0},
		},
	}
	w := newTestWatchdog(t, m, func(c *config) { c.MaxIterations = 2 })
	runWatchdogUntilDone(t, w)
	found := false
	for _, e := range m.emailsSent {
		if strings.Contains(e.Subject, "MWAN TOTAL ALERT") &&
			strings.Contains(e.Body, "Proxmox") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("emails: %+v", m.emailsSent)
	}
	if len(m.vmStopCalls) != 0 {
		t.Fatal("no rollback expected")
	}
}

func TestWatchdog_VMStopped(t *testing.T) {
	m := &mockOps{vmRunning: false}
	w := newTestWatchdog(t, m)
	w.run(context.Background())
	if w.lastState != stateVMStopped {
		t.Fatalf("lastState %q", w.lastState)
	}
	// When VM is stopped with no rollback lock, the watchdog must:
	//   1. Send exactly one alert email.
	//   2. Attempt vmStart exactly once.
	//   3. Not perform any rollback (vmStop).
	//   4. Skip host/guest probes entirely.
	if len(m.emailsSent) != 1 {
		t.Fatalf("expected 1 alert email, got %d: %+v", len(m.emailsSent), m.emailsSent)
	}
	if !strings.Contains(m.emailsSent[0].Subject, "stopped unexpectedly") {
		t.Fatalf("unexpected email subject: %q", m.emailsSent[0].Subject)
	}
	if len(m.vmStartCalls) != 1 {
		t.Fatalf("expected vmStart to be called once, got %d", len(m.vmStartCalls))
	}
	if len(m.vmStopCalls) != 0 {
		t.Fatalf("unexpected vmStop calls: %d", len(m.vmStopCalls))
	}
	if m.pingCallCount != 0 || m.guestExecCallCount != 0 {
		t.Fatalf("VM stopped should skip host/guest probes: ping=%d guest=%d",
			m.pingCallCount, m.guestExecCallCount)
	}
}

func TestWatchdog_VMStopped_RollbackLockSkipsStartAndEmail(t *testing.T) {
	m := &mockOps{vmRunning: false}
	w := newTestWatchdog(t, m)
	// Write the lock file to the path the watchdog already has configured.
	if err := os.WriteFile(w.cfg.RollbackLockFile, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.run(context.Background())
	if w.lastState != stateVMStopped {
		t.Fatalf("lastState %q", w.lastState)
	}
	// Rollback lock present: recoverInterrupted already handled it (called vmStart
	// once to resume the interrupted rollback), then removed the lock. The main
	// loop must NOT send an extra email or issue a second vmStart.
	if len(m.emailsSent) != 0 {
		t.Fatalf("expected no emails when rollback lock present, got %d", len(m.emailsSent))
	}
	if len(m.vmStartCalls) != 1 {
		// recoverInterrupted issues exactly one vmStart for the interrupted rollback.
		t.Fatalf("expected exactly 1 vmStart from recovery, got %d", len(m.vmStartCalls))
	}
}

func TestWatchdog_GuestAgentDown(t *testing.T) {
	nc := testNC()
	m := &mockOps{
		vmRunning:   true,
		pingResults: map[string]bool{"ping:" + nc.PingTargetIPv4: false, "ping6:" + nc.PingTargetIPv6: false},
		guestErr:    errors.New("guest agent unavailable"),
	}
	w := newTestWatchdog(t, m, func(c *config) { c.MaxIterations = 2 })
	runWatchdogUntilDone(t, w)
	if len(m.vmStopCalls) != 0 {
		t.Fatalf("no rollback when guest down: %v", m.vmStopCalls)
	}
}

// ---------------------------------------------------------------------------
// Red-team table
// ---------------------------------------------------------------------------

func TestRedTeamScenarios(t *testing.T) {
	nc := testNC()
	names := make([]string, 0, len(redTeamPresets))
	for n := range redTeamPresets {
		names = append(names, n)
	}
	sort.Strings(names)

	// Build a full set of passing guest results for red-team inner mock.
	baseResults := map[string]guestExecResult{
		guestKeyPing6Default(nc): {ExitCode: 0},
		guestKeyPingDefault(nc):  {ExitCode: 0},
		guestKeyDeployCat(nc): {
			ExitCode: 0,
			Stdout:   strconv.FormatInt(time.Now().Unix()-60, 10),
		},
	}
	for _, k := range allISPKeys(nc) {
		baseResults[k] = guestExecResult{ExitCode: 0}
	}

	for _, name := range names {
		preset := redTeamPresets[name]
		t.Run(name, func(t *testing.T) {
			inner := &mockOps{
				vmRunning: true,
				pingResults: map[string]bool{
					"ping:" + nc.PingTargetIPv4:  true,
					"ping6:" + nc.PingTargetIPv6: true,
				},
				guestResults: baseResults,
				snapshotsOut: []byte("`-> pre-deploy-inner\n"),
			}
			rt := &redTeamOps{
				inner:  inner,
				preset: preset,
				log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
				nc:     nc,
			}
			w := newTestWatchdog(t, rt, func(c *config) {
				c.MaxIterations = 10
				c.ConnectivityTimeoutSeconds = 0
			})

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				w.run(ctx)
				close(done)
			}()
			time.Sleep(100 * time.Millisecond)
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("watchdog did not exit")
			}

			switch name {
			case "ipv4-loss":
				if w.lastState != statePartial {
					t.Fatalf("want partial, got %q", w.lastState)
				}
				if !emailSubjectContains(inner.emailsSent, "IPv4") {
					t.Fatalf("emails %+v", inner.emailsSent)
				}
			case "ipv6-loss":
				if w.lastState != statePartial {
					t.Fatalf("want partial, got %q", w.lastState)
				}
				if !emailSubjectContains(inner.emailsSent, "IPv6") {
					t.Fatalf("emails %+v", inner.emailsSent)
				}
			case "total-loss-mwan":
				if len(inner.vmStopCalls) < 1 {
					t.Fatalf("expected rollback vmStop, got %+v", inner.vmStopCalls)
				}
				if !emailSubjectContains(inner.emailsSent, "AUTO-ROLLBACK") {
					t.Fatalf("emails %+v", inner.emailsSent)
				}
			case "config-drift":
				if len(inner.vmStopCalls) < 1 {
					t.Fatalf("expected rollback vmStop, got %+v", inner.vmStopCalls)
				}
				if !emailSubjectContains(inner.emailsSent, "AUTO-ROLLBACK") {
					t.Fatalf("emails %+v", inner.emailsSent)
				}
				if len(inner.vmRollbackCalls) < 1 ||
					!strings.HasPrefix(inner.vmRollbackCalls[0].Snap, "known-good-") {
					t.Fatalf("want known-good rollback, got %+v", inner.vmRollbackCalls)
				}
			case "total-loss-isp":
				if len(inner.vmStopCalls) != 0 {
					t.Fatalf("expected no rollback, vmStop=%v", inner.vmStopCalls)
				}
			case "vm-crash":
				if w.lastState != stateVMStopped {
					t.Fatalf("want vm_stopped, got %q", w.lastState)
				}
			case "guest-agent-down":
				if len(inner.vmStopCalls) != 0 {
					t.Fatalf("no rollback: %v", inner.vmStopCalls)
				}
			case "proxmox-routing":
				if len(inner.vmStopCalls) != 0 {
					t.Fatalf("no rollback: %v", inner.vmStopCalls)
				}
				found := false
				for _, e := range inner.emailsSent {
					if strings.Contains(e.Subject, "MWAN TOTAL ALERT") &&
						strings.Contains(e.Body, "Proxmox") {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("emails %+v", inner.emailsSent)
				}
			default:
				t.Fatalf("unhandled preset %q", name)
			}
		})
	}
}

func emailSubjectContains(emails []emailSent, sub string) bool {
	for _, e := range emails {
		if strings.Contains(e.Subject, sub) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// watchdogCoord
// ---------------------------------------------------------------------------

func TestWatchdogCoord(t *testing.T) {
	t.Parallel()
	var c watchdogCoord
	if c.isRollingBack() {
		t.Fatal("expected false")
	}
	c.setRollingBack(true)
	if !c.isRollingBack() {
		t.Fatal("expected true")
	}
	c.setRollingBack(false)
	if c.isRollingBack() {
		t.Fatal("expected false")
	}
	c.onSignalDuringRollback()
	if c.takeShutdownAfterRollback() != true {
		t.Fatal("expected deferred shutdown")
	}
	if c.takeShutdownAfterRollback() != false {
		t.Fatal("expected one-shot clear")
	}
}

// ---------------------------------------------------------------------------
// alertLimiter: cooldown remaining + naming alignment with trySend*
// ---------------------------------------------------------------------------

func TestAlertLimiterCooldownRemaining(t *testing.T) {
	t.Parallel()
	l := newAlertLimiter(10)
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if !l.trySendPartial(t0) {
		t.Fatal("first partial")
	}
	if l.partialCooldownRemaining(t0.Add(1*time.Second)) == 0 {
		t.Fatal("expected remaining")
	}
	if l.partialCooldownRemaining(t0.Add(20*time.Second)) != 0 {
		t.Fatal("expected zero after window")
	}
	l.resetCooldowns()
	if l.partialCooldownRemaining(t0) != 0 {
		t.Fatal("after reset")
	}
	if !l.trySendTotal(t0) {
		t.Fatal("first total")
	}
	if l.totalCooldownRemaining(t0.Add(1*time.Second)) == 0 {
		t.Fatal("total remaining")
	}
	if l.totalCooldownRemaining(t0.Add(20*time.Second)) != 0 {
		t.Fatal("total zero")
	}
}

// ---------------------------------------------------------------------------
// watchdog probe helpers
// ---------------------------------------------------------------------------

func TestAppendProbeFlushProbeLog(t *testing.T) {
	t.Parallel()
	w := newTestWatchdog(t, &mockOps{})
	w.appendProbe("a")
	w.appendProbe("b")
	s := w.flushProbeLog()
	if s != "a\nb" {
		t.Fatalf("got %q", s)
	}
	if len(w.probeLog) != 0 {
		t.Fatal("cleared")
	}
}

func TestGuestExecOK(t *testing.T) {
	t.Parallel()
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		guestResults: map[string]guestExecResult{
			guestKeyPing6Default(nc): {ExitCode: 0},
		},
	}
	w := newTestWatchdog(t, m)
	ctx := context.Background()
	if !w.guestExecOK(ctx, "ping6", "-c", "2", "-W", "3", nc.PingTargetIPv6) {
		t.Fatal("expected ok")
	}
	m2 := &mockOps{guestErr: errors.New("boom")}
	w2 := newTestWatchdog(t, m2)
	if w2.guestExecOK(ctx, "ping6", "-c", "2", "-W", "3", nc.PingTargetIPv6) {
		t.Fatal("error path")
	}
	m3 := &mockOps{
		guestResults: map[string]guestExecResult{
			guestKeyPing6Default(nc): {ExitCode: 1},
		},
	}
	w3 := newTestWatchdog(t, m3)
	if w3.guestExecOK(ctx, "ping6", "-c", "2", "-W", "3", nc.PingTargetIPv6) {
		t.Fatal("nonzero exit")
	}
}

func TestProbeConnectivity(t *testing.T) {
	t.Parallel()
	nc := testNC()
	m := &mockOps{
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  true,
			"ping6:" + nc.PingTargetIPv6: false,
		},
	}
	w := newTestWatchdog(t, m)
	v4, v6 := w.probeConnectivity(context.Background())
	if !v4 || v6 {
		t.Fatalf("v4=%v v6=%v", v4, v6)
	}
}

func TestTestVMConnectivity(t *testing.T) {
	t.Parallel()
	nc := testNC()
	t.Run("ipv6 ok", func(t *testing.T) {
		m := &mockOps{
			guestResults: map[string]guestExecResult{
				guestKeyPing6Default(nc): {ExitCode: 0},
			},
		}
		w := newTestWatchdog(t, m)
		if !w.testVMConnectivity(context.Background()) {
			t.Fatal("want true")
		}
	})
	t.Run("ipv4 ok only", func(t *testing.T) {
		m := &mockOps{
			guestResults: map[string]guestExecResult{
				guestKeyPing6Default(nc): {ExitCode: 1},
				guestKeyPingDefault(nc):  {ExitCode: 0},
			},
		}
		w := newTestWatchdog(t, m)
		if !w.testVMConnectivity(context.Background()) {
			t.Fatal("want true")
		}
	})
	t.Run("both fail", func(t *testing.T) {
		m := &mockOps{
			guestResults: map[string]guestExecResult{
				guestKeyPing6Default(nc): {ExitCode: 1},
				guestKeyPingDefault(nc):  {ExitCode: 1},
			},
		}
		w := newTestWatchdog(t, m)
		if w.testVMConnectivity(context.Background()) {
			t.Fatal("want false")
		}
	})
}

func TestTestISP(t *testing.T) {
	t.Parallel()
	nc := testNC()
	t.Run("ipv4 ok first wan", func(t *testing.T) {
		k := guestKeyISP(nc)
		m := &mockOps{
			guestResults: map[string]guestExecResult{
				k: {ExitCode: 0},
			},
		}
		w := newTestWatchdog(t, m)
		if !w.testISP(context.Background()) {
			t.Fatal("want true")
		}
	})
	t.Run("ipv6 ok when v4 fails", func(t *testing.T) {
		k4 := strings.Join(
			[]string{"ping", "-c", "3", "-W", "3", "-I", nc.WANInterfaces[0].Name, nc.PingTargetIPv4},
			" ",
		)
		k6 := strings.Join(
			[]string{"ping6", "-c", "3", "-W", "3", "-I", nc.WANInterfaces[0].Name, nc.PingTargetIPv6},
			" ",
		)
		m := &mockOps{
			guestResults: map[string]guestExecResult{
				k4: {ExitCode: 1},
				k6: {ExitCode: 0},
			},
		}
		w := newTestWatchdog(t, m)
		if !w.testISP(context.Background()) {
			t.Fatal("want true")
		}
	})
	t.Run("all interfaces fail", func(t *testing.T) {
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{}
		for _, k := range allISPKeys(nc) {
			res[k] = fail
		}
		m := &mockOps{guestResults: res}
		w := newTestWatchdog(t, m)
		if w.testISP(context.Background()) {
			t.Fatal("want false")
		}
	})
}

func TestFindSnapshot(t *testing.T) {
	t.Parallel()
	t.Run("listsnapshot error", func(t *testing.T) {
		m := &mockOps{vmSnapshotsErr: errors.New("qm fail")}
		w := newTestWatchdog(t, m)
		_, err := w.findSnapshot(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("empty extract", func(t *testing.T) {
		m := &mockOps{snapshotsOut: []byte("no snapshot\n")}
		w := newTestWatchdog(t, m)
		s, err := w.findSnapshot(context.Background())
		if err != nil || s != "" {
			t.Fatalf("got %q %v", s, err)
		}
	})
	t.Run("found", func(t *testing.T) {
		m := &mockOps{snapshotsOut: []byte("`-> pre-deploy-x\n")}
		w := newTestWatchdog(t, m)
		s, err := w.findSnapshot(context.Background())
		if err != nil || s != "pre-deploy-x" {
			t.Fatalf("got %q %v", s, err)
		}
	})
	t.Run("known-good only", func(t *testing.T) {
		m := &mockOps{snapshotsOut: []byte("`-> known-good-x\n")}
		w := newTestWatchdog(t, m)
		s, err := w.findSnapshot(context.Background())
		if err != nil || s != "known-good-x" {
			t.Fatalf("got %q %v", s, err)
		}
	})
}

func TestCheckDeploy(t *testing.T) {
	t.Parallel()
	nc := testNC()
	ctx := context.Background()
	t.Run("vmStatus error", func(t *testing.T) {
		m := &mockOps{vmStatusErr: errors.New("status")}
		w := newTestWatchdog(t, m)
		ts, ok := w.checkDeploy(ctx)
		if ok || ts != 0 {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("vm not running", func(t *testing.T) {
		m := &mockOps{vmRunning: false}
		w := newTestWatchdog(t, m)
		ts, ok := w.checkDeploy(ctx)
		if ok || ts != 0 {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("guestExec error", func(t *testing.T) {
		m := &mockOps{vmRunning: true, guestErr: errors.New("guest")}
		w := newTestWatchdog(t, m)
		ts, ok := w.checkDeploy(ctx)
		if ok || ts != 0 {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("cat nonzero", func(t *testing.T) {
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyDeployCat(nc): {ExitCode: 1},
			},
		}
		w := newTestWatchdog(t, m)
		ts, ok := w.checkDeploy(ctx)
		if ok || ts != 0 {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("empty stdout", func(t *testing.T) {
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyDeployCat(nc): {ExitCode: 0, Stdout: "  \n"},
			},
		}
		w := newTestWatchdog(t, m)
		ts, ok := w.checkDeploy(ctx)
		if ok || ts != 0 {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("null stdout", func(t *testing.T) {
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyDeployCat(nc): {ExitCode: 0, Stdout: "null"},
			},
		}
		w := newTestWatchdog(t, m)
		ts, ok := w.checkDeploy(ctx)
		if ok || ts != 0 {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("bad timestamp", func(t *testing.T) {
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyDeployCat(nc): {ExitCode: 0, Stdout: "not-a-number"},
			},
		}
		w := newTestWatchdog(t, m)
		ts, ok := w.checkDeploy(ctx)
		if ok || ts != 0 {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("stale deploy", func(t *testing.T) {
		old := time.Now().Unix() - 3600
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyDeployCat(nc): {ExitCode: 0, Stdout: strconv.FormatInt(old, 10)},
			},
		}
		w := newTestWatchdog(t, m, func(c *config) { c.DeployWindowMinutes = 30 })
		ts, ok := w.checkDeploy(ctx)
		if ok || ts != 0 {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("recent deploy", func(t *testing.T) {
		recent := time.Now().Unix() - 60
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyDeployCat(nc): {ExitCode: 0, Stdout: strconv.FormatInt(recent, 10)},
			},
		}
		w := newTestWatchdog(t, m, func(c *config) { c.DeployWindowMinutes = 30 })
		ts, ok := w.checkDeploy(ctx)
		if !ok || ts != recent {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
	t.Run("recent change marker only", func(t *testing.T) {
		recent := time.Now().Unix() - 60
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyDeployCat(nc): {ExitCode: 1},
				guestKeyChangeCat(nc): {ExitCode: 0, Stdout: strconv.FormatInt(recent, 10)},
			},
		}
		w := newTestWatchdog(t, m, func(c *config) { c.DeployWindowMinutes = 30 })
		ts, ok := w.checkDeploy(ctx)
		if !ok || ts != recent {
			t.Fatalf("ts=%d ok=%v", ts, ok)
		}
	})
}

func TestCheckConfigHash(t *testing.T) {
	t.Parallel()
	nc := testNC()
	ctx := context.Background()
	t.Run("first run records hash no drift", func(t *testing.T) {
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyConfigHashCat(nc): {ExitCode: 0, Stdout: "aaa\n"},
			},
		}
		w := newTestWatchdog(t, m)
		w.nc = nc
		w.checkConfigHash(ctx)
		if w.lastConfigHash != "aaa" {
			t.Fatalf("got %q", w.lastConfigHash)
		}
		if w.hashChangeWindowStart != 0 {
			t.Fatal("want no drift window")
		}
	})
	t.Run("hash change sets window", func(t *testing.T) {
		m := &mockOps{
			vmRunning: true,
			guestResults: map[string]guestExecResult{
				guestKeyConfigHashCat(nc): {ExitCode: 0, Stdout: "bbb\n"},
			},
		}
		w := newTestWatchdog(t, m)
		w.nc = nc
		w.lastConfigHash = "aaa"
		w.checkConfigHash(ctx)
		if w.hashChangeWindowStart == 0 {
			t.Fatal("want hash window")
		}
	})
}

func TestMaybeSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockOps{
		vmRunning:    true,
		snapshotsOut: []byte(""),
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.SnapshotHealthyThreshold = 2
		c.MinSnapshotIntervalSeconds = 0
		c.MaxKnownGoodSnapshots = 0
		c.MaxTotalSnapshots = 0
	})
	w.consecutiveHealthy = 1
	w.maybeSnapshot(ctx)
	if len(m.vmSnapshotCalls) != 0 {
		t.Fatalf("no snapshot yet")
	}
	w.consecutiveHealthy = 2
	w.maybeSnapshot(ctx)
	if len(m.vmSnapshotCalls) != 1 ||
		!strings.HasPrefix(m.vmSnapshotCalls[0].Name, "known-good-") {
		t.Fatalf("got %+v", m.vmSnapshotCalls)
	}
}

func TestPruneSnapshots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockOps{
		vmRunning: true,
		snapshotsOut: []byte(
			"`-> known-good-001\n`-> known-good-002\n`-> known-good-003\n`-> known-good-004\n",
		),
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.MaxKnownGoodSnapshots = 2
		c.MaxTotalSnapshots = 99
	})
	if err := w.pruneSnapshots(ctx); err != nil {
		t.Fatal(err)
	}
	if len(m.vmDelSnapshotCalls) != 2 {
		t.Fatalf("got %+v", m.vmDelSnapshotCalls)
	}
}

func TestRollback(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	m := &mockOps{}
	w := newTestWatchdog(t, m, func(c *config) {
		c.RollbackLockFile = filepath.Join(tmp, "rollback.lock")
		c.RollbackStateFile = filepath.Join(tmp, "rollback.state")
		c.PostRollbackGraceSeconds = 0
	})
	w.rollback(context.Background(), 42, "snap-a")
	if len(m.vmStopCalls) != 1 || len(m.vmRollbackCalls) != 1 || len(m.vmStartCalls) != 1 {
		t.Fatalf("calls stop=%v rb=%v start=%v", m.vmStopCalls, m.vmRollbackCalls, m.vmStartCalls)
	}
	_, err := os.Stat(w.cfg.RollbackLockFile)
	if err == nil || !os.IsNotExist(err) {
		t.Fatal("lock should be removed")
	}
	ds, done, snap, err := parseRollbackStateFile(w.cfg.RollbackStateFile)
	if err != nil || ds != "42" || !done || snap != "snap-a" {
		t.Fatalf("state %q %v %q err=%v", ds, done, snap, err)
	}
}

func TestRollback_ErrorsContinue(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	m := &mockOps{
		vmStopErr:     errors.New("stop"),
		vmRollbackErr: errors.New("rb"),
		vmStartErr:    errors.New("start"),
		sendEmailErr:  errors.New("mail"),
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.RollbackLockFile = filepath.Join(tmp, "rollback.lock")
		c.RollbackStateFile = filepath.Join(tmp, "rollback.state")
		c.PostRollbackGraceSeconds = 0
	})
	w.rollback(context.Background(), 7, "snap-b")
	if len(m.emailsSent) != 0 {
		t.Fatal("email error path")
	}
}

func TestRollback_LockWriteFails(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	blockFile := filepath.Join(tmp, "block")
	if err := os.WriteFile(blockFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(blockFile, "nested.lock")
	m := &mockOps{}
	w := newTestWatchdog(t, m, func(c *config) {
		c.RollbackLockFile = lockPath
		c.RollbackStateFile = filepath.Join(tmp, "ok.state")
	})
	w.rollback(context.Background(), 1, "snap")
	if _, err := os.Stat(w.cfg.RollbackStateFile); err != nil {
		t.Fatalf("state written: %v", err)
	}
}

func TestRecoverInterrupted(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	lockPath := filepath.Join(tmp, "lock")
	ctx := context.Background()
	t.Run("no lock file", func(t *testing.T) {
		m := &mockOps{vmRunning: true}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackLockFile = filepath.Join(tmp, "missing-lock")
			c.PostRollbackGraceSeconds = 0
		})
		w.recoverInterrupted(ctx)
	})
	t.Run("lock exists vm running removes", func(t *testing.T) {
		if err := os.WriteFile(lockPath, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := &mockOps{vmRunning: true}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackLockFile = lockPath
			c.PostRollbackGraceSeconds = 0
		})
		w.recoverInterrupted(ctx)
		if _, err := os.Stat(lockPath); err == nil || !os.IsNotExist(err) {
			t.Fatal("lock removed")
		}
	})
	t.Run("lock exists vm stopped starts", func(t *testing.T) {
		lp := filepath.Join(tmp, "lock2")
		if err := os.WriteFile(lp, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := &mockOps{vmRunning: false}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackLockFile = lp
			c.PostRollbackGraceSeconds = 0
		})
		w.recoverInterrupted(ctx)
		if len(m.vmStartCalls) != 1 {
			t.Fatalf("start %v", m.vmStartCalls)
		}
	})
	t.Run("read error not notexist", func(t *testing.T) {
		dirPath := filepath.Join(tmp, "lockdir")
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatal(err)
		}
		m := &mockOps{}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackLockFile = dirPath
		})
		w.recoverInterrupted(ctx)
	})
	t.Run("vmStatus error", func(t *testing.T) {
		lp := filepath.Join(tmp, "lock3")
		if err := os.WriteFile(lp, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := &mockOps{vmStatusErr: errors.New("st")}
		w := newTestWatchdog(t, m, func(c *config) { c.RollbackLockFile = lp })
		w.recoverInterrupted(ctx)
	})
	t.Run("vmStart fails", func(t *testing.T) {
		lp := filepath.Join(tmp, "lock4")
		if err := os.WriteFile(lp, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := &mockOps{vmRunning: false, vmStartErr: errors.New("no start")}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackLockFile = lp
			c.PostRollbackGraceSeconds = 0
		})
		w.recoverInterrupted(ctx)
	})
}

func TestSendPartialAlert(t *testing.T) {
	t.Parallel()
	nc := testNC()
	m := &mockOps{}
	w := newTestWatchdog(t, m, func(c *config) { c.AlertCooldownSeconds = 300 })
	w.nc = nc
	w.appendProbe("probe1")
	w.sendPartialAlert(context.Background(), "IPv4")
	if len(m.emailsSent) != 1 {
		t.Fatalf("emails %+v", m.emailsSent)
	}
	w.sendPartialAlert(context.Background(), "IPv4")
	if len(m.emailsSent) != 1 {
		t.Fatal("cooldown suppress")
	}
}

func TestSendPartialAlert_EmailError(t *testing.T) {
	t.Parallel()
	m := &mockOps{sendEmailErr: errors.New("smtp")}
	w := newTestWatchdog(t, m, func(c *config) { c.AlertCooldownSeconds = 0 })
	w.sendPartialAlert(context.Background(), "IPv6")
}

func TestSendTotalAlert_CooldownAndError(t *testing.T) {
	t.Parallel()
	m := &mockOps{}
	w := newTestWatchdog(t, m, func(c *config) { c.AlertCooldownSeconds = 600 })
	w.consecutiveTotalFails = 2
	w.sendTotalAlert(context.Background(), "r", "d")
	w.sendTotalAlert(context.Background(), "r", "d")
	if len(m.emailsSent) != 1 {
		t.Fatal("one email")
	}
	m2 := &mockOps{sendEmailErr: errors.New("e")}
	w2 := newTestWatchdog(t, m2, func(c *config) { c.AlertCooldownSeconds = 0 })
	w2.sendTotalAlert(context.Background(), "r", "d")
}

func TestPingTarget(t *testing.T) {
	t.Parallel()
	w := newTestWatchdog(t, &mockOps{})
	w.nc = testNC()
	if w.pingTarget("IPv6") != w.nc.PingTargetIPv6 {
		t.Fatal("v6")
	}
	if w.pingTarget("IPv4") != w.nc.PingTargetIPv4 {
		t.Fatal("v4")
	}
}

func TestSleepOrDone(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepOrDone(ctx, time.Second) {
		t.Fatal("cancelled")
	}
	if !sleepOrDone(ctx, 0) {
		t.Fatal("zero duration")
	}
	ctx2 := context.Background()
	if !sleepOrDone(ctx2, 5*time.Millisecond) {
		t.Fatal("sleep completed")
	}
}

// cancelledCtx skips long sleepOrDone(60s) branches in handleTimeoutExceeded.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestHandleTimeoutExceeded(t *testing.T) {
	t.Parallel()
	nc := testNC()
	ctx := cancelledCtx()
	t.Run("proxmox routing", func(t *testing.T) {
		m := &mockOps{
			guestResults: map[string]guestExecResult{
				guestKeyPing6Default(nc): {ExitCode: 0},
			},
		}
		w := newTestWatchdog(t, m, func(c *config) {
			c.ConnectivityTimeoutSeconds = 0
			c.AlertCooldownSeconds = 0
		})
		w.nc = nc
		w.handleTimeoutExceeded(ctx)
		if !emailSubjectContains(m.emailsSent, "MWAN TOTAL ALERT") {
			t.Fatalf("emails %+v", m.emailsSent)
		}
	})
	t.Run("isp outage fail count lte 2", func(t *testing.T) {
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{
			guestKeyPing6Default(nc): fail,
			guestKeyPingDefault(nc):  fail,
		}
		for _, k := range allISPKeys(nc) {
			res[k] = fail
		}
		m := &mockOps{guestResults: res}
		w := newTestWatchdog(t, m, func(c *config) { c.AlertCooldownSeconds = 0 })
		w.nc = nc
		w.consecutiveTotalFails = 1
		w.handleTimeoutExceeded(ctx)
	})
	t.Run("isp outage fail count gt 2", func(t *testing.T) {
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{
			guestKeyPing6Default(nc): fail,
			guestKeyPingDefault(nc):  fail,
		}
		for _, k := range allISPKeys(nc) {
			res[k] = fail
		}
		m := &mockOps{guestResults: res}
		w := newTestWatchdog(t, m, func(c *config) { c.AlertCooldownSeconds = 0 })
		w.nc = nc
		w.consecutiveTotalFails = 5
		w.handleTimeoutExceeded(ctx)
	})
	t.Run("no recent deploy", func(t *testing.T) {
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{
			guestKeyPing6Default(nc): fail,
			guestKeyPingDefault(nc):  fail,
		}
		for _, k := range allISPKeys(nc) {
			res[k] = guestExecResult{ExitCode: 0}
		}
		old := time.Now().Unix() - 7200
		res[guestKeyDeployCat(nc)] = guestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(old, 10),
		}
		m := &mockOps{vmRunning: true, guestResults: res}
		w := newTestWatchdog(t, m, func(c *config) {
			c.DeployWindowMinutes = 30
			c.AlertCooldownSeconds = 0
		})
		w.nc = nc
		w.handleTimeoutExceeded(ctx)
	})
	t.Run("rollback already done", func(t *testing.T) {
		tmp := t.TempDir()
		st := filepath.Join(tmp, "st")
		recent := time.Now().Unix() - 60
		if err := writeRollbackState(st, recent, "snap"); err != nil {
			t.Fatal(err)
		}
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{
			guestKeyPing6Default(nc): fail,
			guestKeyPingDefault(nc):  fail,
		}
		for _, k := range allISPKeys(nc) {
			res[k] = guestExecResult{ExitCode: 0}
		}
		res[guestKeyDeployCat(nc)] = guestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(recent, 10),
		}
		m := &mockOps{vmRunning: true, guestResults: res}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackStateFile = st
			c.AlertCooldownSeconds = 0
		})
		w.nc = nc
		w.handleTimeoutExceeded(ctx)
	})
	t.Run("listsnapshot error", func(t *testing.T) {
		recent := time.Now().Unix() - 60
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{
			guestKeyPing6Default(nc): fail,
			guestKeyPingDefault(nc):  fail,
		}
		for _, k := range allISPKeys(nc) {
			res[k] = guestExecResult{ExitCode: 0}
		}
		res[guestKeyDeployCat(nc)] = guestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(recent, 10),
		}
		m := &mockOps{
			vmRunning:      true,
			guestResults:   res,
			vmSnapshotsErr: errors.New("snap err"),
		}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackStateFile = filepath.Join(t.TempDir(), "none")
			c.AlertCooldownSeconds = 0
		})
		w.nc = nc
		w.handleTimeoutExceeded(ctx)
	})
	t.Run("no snapshot", func(t *testing.T) {
		recent := time.Now().Unix() - 60
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{
			guestKeyPing6Default(nc): fail,
			guestKeyPingDefault(nc):  fail,
		}
		for _, k := range allISPKeys(nc) {
			res[k] = guestExecResult{ExitCode: 0}
		}
		res[guestKeyDeployCat(nc)] = guestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(recent, 10),
		}
		m := &mockOps{vmRunning: true, guestResults: res, snapshotsOut: []byte("none")}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackStateFile = filepath.Join(t.TempDir(), "fresh")
			c.AlertCooldownSeconds = 0
		})
		w.nc = nc
		w.handleTimeoutExceeded(ctx)
		if len(m.emailsSent) != 1 ||
			!strings.Contains(m.emailsSent[0].Body, "known-good") {
			t.Fatalf("emails %+v", m.emailsSent)
		}
	})
	t.Run("rollback path", func(t *testing.T) {
		tmp := t.TempDir()
		recent := time.Now().Unix() - 60
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{
			guestKeyPing6Default(nc): fail,
			guestKeyPingDefault(nc):  fail,
		}
		for _, k := range allISPKeys(nc) {
			res[k] = guestExecResult{ExitCode: 0}
		}
		res[guestKeyDeployCat(nc)] = guestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(recent, 10),
		}
		m := &mockOps{
			vmRunning:    true,
			guestResults: res,
			snapshotsOut: []byte("`-> pre-deploy-z\n"),
		}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackStateFile = filepath.Join(tmp, "rs.state")
			c.RollbackLockFile = filepath.Join(tmp, "rs.lock")
			c.PostRollbackGraceSeconds = 0
			c.AlertCooldownSeconds = 0
		})
		w.nc = nc
		w.handleTimeoutExceeded(ctx)
		if len(m.vmRollbackCalls) != 1 {
			t.Fatalf("rollback %+v", m.vmRollbackCalls)
		}
	})
	t.Run("rollback state read error logs", func(t *testing.T) {
		recent := time.Now().Unix() - 60
		fail := guestExecResult{ExitCode: 1}
		res := map[string]guestExecResult{
			guestKeyPing6Default(nc): fail,
			guestKeyPingDefault(nc):  fail,
		}
		for _, k := range allISPKeys(nc) {
			res[k] = guestExecResult{ExitCode: 0}
		}
		res[guestKeyDeployCat(nc)] = guestExecResult{
			ExitCode: 0,
			Stdout:   strconv.FormatInt(recent, 10),
		}
		dirPath := filepath.Join(t.TempDir(), "statedir")
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatal(err)
		}
		m := &mockOps{vmRunning: true, guestResults: res, snapshotsOut: []byte("`-> pre-deploy-y\n")}
		w := newTestWatchdog(t, m, func(c *config) {
			c.RollbackStateFile = dirPath
			c.RollbackLockFile = filepath.Join(t.TempDir(), "l.lock")
			c.PostRollbackGraceSeconds = 0
			c.AlertCooldownSeconds = 0
		})
		w.nc = nc
		w.handleTimeoutExceeded(ctx)
	})
}

// ---------------------------------------------------------------------------
// ops.go helpers + dryRunOps + newRealOps
// ---------------------------------------------------------------------------

func TestPingArgHelpers(t *testing.T) {
	t.Parallel()
	if got := pingTarget([]string{"ping", "8.8.8.8"}); got != "8.8.8.8" {
		t.Fatalf("pingTarget %q", got)
	}
	if got := pingIface([]string{"ping", "-c", "3", "-W", "3", "-I", "eth0", "1.1.1.1"}); got != "eth0" {
		t.Fatalf("pingIface %q", got)
	}
	if n := pingCount([]string{"ping6", "-c", "5", "x"}, 2); n != 5 {
		t.Fatalf("pingCount %d", n)
	}
	if n := pingCount([]string{"ping", "-c", "bad", "x"}, 3); n != 3 {
		t.Fatalf("bad count %d", n)
	}
}

func TestWanIfaceNames(t *testing.T) {
	t.Parallel()
	nc := networkConfig{
		WANInterfaces: []WANInterface{{Name: "a"}, {Name: "b"}},
	}
	names := nc.wanIfaceNames()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("%v", names)
	}
}

func TestNewRealOps(t *testing.T) {
	t.Parallel()
	cfg := config{}
	nc := defaultNetworkConfig()
	r := newRealOps(cfg, nc)
	if r.pve != nil {
		t.Fatal("nil pve without token")
	}
	cfg.PVETokenID = "x"
	cfg.PVESecret = "y"
	cfg.PVEBaseURL = "https://example/api"
	r2 := newRealOps(cfg, nc)
	if r2.pve == nil {
		t.Fatal("expected client")
	}
}

func TestDryRunOps(t *testing.T) {
	t.Parallel()
	inner := &mockOps{
		vmRunning:    true,
		snapshotsOut: []byte("x"),
		pingResults:  map[string]bool{"ping:1.1.1.1": true},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &dryRunOps{inner: inner, log: log}
	ctx := context.Background()
	if running, err := d.vmStatus(ctx, "113"); err != nil || !running {
		t.Fatalf("%v %v", running, err)
	}
	if err := d.vmStop(ctx, "113"); err != nil {
		t.Fatal(err)
	}
	if err := d.vmRollback(ctx, "113", "s"); err != nil {
		t.Fatal(err)
	}
	if err := d.vmStart(ctx, "113"); err != nil {
		t.Fatal(err)
	}
	if err := d.vmSnapshot(ctx, "113", "snap"); err != nil {
		t.Fatal(err)
	}
	if err := d.vmDelSnapshot(ctx, "113", "snap"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.vmSnapshots(ctx, "113"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.guestExec(ctx, "113", "echo"); err != nil {
		t.Fatal(err)
	}
	if !d.ping(ctx, "ping", "1.1.1.1") {
		t.Fatal("ping")
	}
	if err := d.sendEmail(ctx, "a", "sub", "body"); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// logging.go
// ---------------------------------------------------------------------------

func TestTeeHandlerAndTextHandler(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	th := &textHandler{w: &buf}
	h2 := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})
	tee := newTeeHandler(th, h2)
	ctx := context.Background()
	if !tee.Enabled(ctx, slog.LevelInfo) {
		t.Fatal("enabled")
	}
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hi", 0)
	rec.Add("k", 1)
	if err := tee.Handle(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "hi") {
		t.Fatalf("buf %q", buf.String())
	}
	withG := tee.WithGroup("g")
	if withG == nil {
		t.Fatal("withGroup")
	}
	withA := tee.WithAttrs([]slog.Attr{slog.String("a", "b")})
	if withA == nil {
		t.Fatal("withAttrs")
	}
}

func TestNewWatchdogLogger(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	cfg := config{
		LogFile:     filepath.Join(tmp, "a.log"),
		JSONLogFile: filepath.Join(tmp, "b.jsonl"),
	}
	lg, err := newWatchdogLogger(cfg)
	if err != nil || lg == nil {
		t.Fatalf("logger %v", err)
	}
	lg.Info("test", "n", 1)
	b1, err := os.ReadFile(cfg.LogFile)
	if err != nil || len(b1) == 0 {
		t.Fatalf("text log: %v len=%d", err, len(b1))
	}
	b2, err := os.ReadFile(cfg.JSONLogFile)
	if err != nil || len(b2) == 0 {
		t.Fatalf("json log: %v len=%d", err, len(b2))
	}
}

// ---------------------------------------------------------------------------
// config.go extra branches
// ---------------------------------------------------------------------------

func TestLoadConfigWarningsAndInts(t *testing.T) {
	t.Setenv("MWAN_VMID", "")
	t.Setenv("DEPLOY_WINDOW_MINUTES", "not-int")
	t.Setenv("CONNECTIVITY_TIMEOUT_SECONDS", "bad")
	t.Setenv("CHECK_INTERVAL_HEALTHY", "x")
	t.Setenv("CHECK_INTERVAL_DEGRADED", "y")
	t.Setenv("POST_ROLLBACK_GRACE_SECONDS", "z")
	t.Setenv("ALERT_COOLDOWN_SECONDS", "w")
	t.Setenv("MWAN_VSOCK_CID", "abc")
	t.Setenv("MWAN_VSOCK_PORT", "def")
	t.Setenv("SMTP2GO_API_KEY", "")
	cfg, err := loadConfig(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ConfigWarnings) == 0 {
		t.Fatal("expected warnings")
	}
	if cfg.VsockCID != defaultVsockCID || cfg.VsockPort != defaultVsockPort {
		t.Fatalf("vsock defaults %d %d", cfg.VsockCID, cfg.VsockPort)
	}
}

func TestLoadNetworkConfigReadError(t *testing.T) {
	t.Parallel()
	_, err := loadNetworkConfig("/")
	if err == nil {
		t.Fatal("expected read error for directory path")
	}
}

func TestLoadNetworkConfigInvalidTOML(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(p, []byte("<<<"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadNetworkConfig(p)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// ---------------------------------------------------------------------------
// rollback.go error paths
// ---------------------------------------------------------------------------

func TestParseRollbackStateFile_Error(t *testing.T) {
	t.Parallel()
	_, _, _, err := parseRollbackStateFile(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRollbackAlreadyDone_ReadError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "isdir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := rollbackAlreadyDone(dirPath, 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// redTeamOps per-method fault toggles
// ---------------------------------------------------------------------------

func TestRedTeamOps_VMStatus(t *testing.T) {
	t.Parallel()
	inner := &mockOps{vmRunning: true}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := &redTeamOps{
		inner:  inner,
		preset: redTeamPreset{VMStopped: true},
		log:    log,
		nc:     testNC(),
	}
	if running, err := rt.vmStatus(context.Background(), "113"); err != nil || running {
		t.Fatalf("injected stopped: %v %v", running, err)
	}
	rtOff := &redTeamOps{inner: inner, preset: redTeamPreset{}, log: log, nc: testNC()}
	if running, err := rtOff.vmStatus(context.Background(), "113"); err != nil || !running {
		t.Fatalf("passthrough %v %v", running, err)
	}
}

func TestRedTeamOps_VMLifecyclePassthrough(t *testing.T) {
	t.Parallel()
	inner := &mockOps{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := &redTeamOps{inner: inner, log: log, nc: testNC()}
	ctx := context.Background()
	_ = rt.vmStop(ctx, "1")
	_ = rt.vmRollback(ctx, "1", "s")
	_ = rt.vmStart(ctx, "1")
	_ = rt.vmSnapshot(ctx, "1", "snap")
	_ = rt.vmDelSnapshot(ctx, "1", "snap")
	if len(inner.vmStopCalls) != 1 {
		t.Fatal("stop")
	}
	if len(inner.vmSnapshotCalls) != 1 || inner.vmSnapshotCalls[0].Name != "snap" {
		t.Fatalf("snapshot %+v", inner.vmSnapshotCalls)
	}
	if len(inner.vmDelSnapshotCalls) != 1 || inner.vmDelSnapshotCalls[0].Name != "snap" {
		t.Fatalf("delsnap %+v", inner.vmDelSnapshotCalls)
	}
}

func TestRedTeamOps_VMSnapshotsInject(t *testing.T) {
	t.Parallel()
	inner := &mockOps{snapshotsOut: []byte("inner")}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := &redTeamOps{
		inner:  inner,
		preset: redTeamPreset{InjectSnapshot: true},
		log:    log,
		nc:     testNC(),
	}
	out, err := rt.vmSnapshots(context.Background(), "113")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "pre-deploy-") {
		t.Fatalf("got %s", out)
	}
	rtOff := &redTeamOps{inner: inner, preset: redTeamPreset{}, log: log, nc: testNC()}
	out2, err := rtOff.vmSnapshots(context.Background(), "113")
	if err != nil || string(out2) != "inner" {
		t.Fatalf("%s %v", out2, err)
	}
}

func TestRedTeamOps_GuestExecBranches(t *testing.T) {
	t.Parallel()
	nc := testNC()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	inner := &mockOps{}
	t.Run("guest exec fail", func(t *testing.T) {
		rt := &redTeamOps{
			inner:  inner,
			preset: redTeamPreset{GuestExecFail: true},
			log:    log,
			nc:     nc,
		}
		_, err := rt.guestExec(ctx, "113", "ping", "x")
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("iface fail", func(t *testing.T) {
		rt := &redTeamOps{
			inner:  inner,
			preset: redTeamPreset{GuestIfaceFail: true},
			log:    log,
			nc:     nc,
		}
		args := []string{"ping", "-c", "3", "-W", "3", "-I", "eth0", nc.PingTargetIPv4}
		r, err := rt.guestExec(ctx, "113", args...)
		if err != nil || r.ExitCode == 0 {
			t.Fatalf("r=%v err=%v", r, err)
		}
	})
	t.Run("iface succeed", func(t *testing.T) {
		rt := &redTeamOps{
			inner:  inner,
			preset: redTeamPreset{GuestIfaceSucceed: true},
			log:    log,
			nc:     nc,
		}
		args := []string{"ping", "-c", "3", "-W", "3", "-I", "eth0", nc.PingTargetIPv4}
		r, err := rt.guestExec(ctx, "113", args...)
		if err != nil || r.ExitCode != 0 {
			t.Fatalf("r=%v err=%v", r, err)
		}
	})
	t.Run("default fail", func(t *testing.T) {
		rt := &redTeamOps{
			inner:  inner,
			preset: redTeamPreset{GuestDefaultFail: true},
			log:    log,
			nc:     nc,
		}
		r, err := rt.guestExec(ctx, "113", "ping6", "-c", "2", "-W", "3", nc.PingTargetIPv6)
		if err != nil || r.ExitCode == 0 {
			t.Fatalf("r=%v err=%v", r, err)
		}
	})
	t.Run("inject deploy ts", func(t *testing.T) {
		rt := &redTeamOps{
			inner:  inner,
			preset: redTeamPreset{InjectDeployTS: true},
			log:    log,
			nc:     nc,
		}
		r, err := rt.guestExec(ctx, "113", "cat", nc.LastDeployPath)
		if err != nil || r.ExitCode != 0 || r.Stdout == "" {
			t.Fatalf("r=%+v err=%v", r, err)
		}
	})
	t.Run("passthrough", func(t *testing.T) {
		inn := &mockOps{
			guestResults: map[string]guestExecResult{
				"echo hello": {ExitCode: 0},
			},
		}
		rt := &redTeamOps{inner: inn, preset: redTeamPreset{}, log: log, nc: nc}
		r, err := rt.guestExec(ctx, "113", "echo", "hello")
		if err != nil || r.ExitCode != 0 {
			t.Fatalf("%+v %v", r, err)
		}
	})
}

func TestRedTeamOps_Ping(t *testing.T) {
	t.Parallel()
	nc := testNC()
	inner := &mockOps{
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  true,
			"ping6:" + nc.PingTargetIPv6: true,
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Run("host v4 fail", func(t *testing.T) {
		rt := &redTeamOps{
			inner:  inner,
			preset: redTeamPreset{HostV4Fail: true},
			log:    log,
			nc:     nc,
		}
		if rt.ping(context.Background(), "ping", nc.PingTargetIPv4) {
			t.Fatal("should fail")
		}
	})
	t.Run("host v6 fail", func(t *testing.T) {
		rt := &redTeamOps{
			inner:  inner,
			preset: redTeamPreset{HostV6Fail: true},
			log:    log,
			nc:     nc,
		}
		if rt.ping(context.Background(), "ping6", nc.PingTargetIPv6) {
			t.Fatal("should fail")
		}
	})
	t.Run("passthrough", func(t *testing.T) {
		rt := &redTeamOps{inner: inner, preset: redTeamPreset{}, log: log, nc: nc}
		if !rt.ping(context.Background(), "ping", nc.PingTargetIPv4) {
			t.Fatal("want ok")
		}
	})
}

func TestRedTeamOps_SendEmail(t *testing.T) {
	t.Parallel()
	inner := &mockOps{}
	rt := &redTeamOps{
		inner: inner,
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		nc:    testNC(),
	}
	if err := rt.sendEmail(context.Background(), "a", "s", "b"); err != nil {
		t.Fatal(err)
	}
	if len(inner.emailsSent) != 1 {
		t.Fatal("forwarded")
	}
}

// ---------------------------------------------------------------------------
// run() branches + realOps / runCmd
// ---------------------------------------------------------------------------

func TestWatchdog_Run_ContextCancelledImmediately(t *testing.T) {
	t.Parallel()
	w := newTestWatchdog(t, &mockOps{vmRunning: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.run(ctx)
}

func TestWatchdog_Run_MaxIterationsOne(t *testing.T) {
	t.Parallel()
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  true,
			"ping6:" + nc.PingTargetIPv6: true,
		},
	}
	w := newTestWatchdog(t, m, func(c *config) { c.MaxIterations = 1 })
	w.run(context.Background())
}

func TestWatchdog_Run_VMStatusErrorThenCancel(t *testing.T) {
	t.Parallel()
	m := &mockOps{vmStatusErr: errors.New("qm")}
	w := newTestWatchdog(t, m, func(c *config) {
		c.CheckIntervalDegraded = 5 * time.Millisecond
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	w.run(ctx)
}

func TestWatchdog_Run_VMStoppedThenRunning(t *testing.T) {
	t.Parallel()
	nc := testNC()
	stoppedRounds := 0
	m := &mockOps{
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  true,
			"ping6:" + nc.PingTargetIPv6: true,
		},
		vmStatusFn: func(context.Context, string) (bool, error) {
			if stoppedRounds < 2 {
				stoppedRounds++
				return false, nil
			}
			return true, nil
		},
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.MaxIterations = 8
		c.CheckIntervalDegraded = 0
		c.CheckIntervalHealthy = 0
	})
	w.run(context.Background())
}

func TestRunCmd(t *testing.T) {
	t.Parallel()
	out, err := runCmd(context.Background(), time.Second, "echo", "mwan")
	if err != nil {
		t.Fatalf("runCmd: %v", err)
	}
	if !strings.Contains(string(out), "mwan") {
		t.Fatalf("output %q", out)
	}
}

func TestRealOpsPing(t *testing.T) {
	t.Parallel()
	r := newRealOps(config{}, defaultNetworkConfig())
	ctx := context.Background()
	if !r.ping(ctx, "ping", "127.0.0.1") {
		t.Skip("ICMP ping to 127.0.0.1 failed on this host")
	}
	if r.ping(ctx, "this-binary-does-not-exist-xyz", "127.0.0.1") {
		t.Fatal("expected failure for missing ping binary")
	}
}

func TestRealOpsGuestExec_NoPVE(t *testing.T) {
	t.Parallel()
	r := newRealOps(config{}, defaultNetworkConfig())
	_, err := r.guestExec(context.Background(), "113", "ping", "127.0.0.1")
	if err == nil {
		t.Fatal("expected error without vsock/PVE")
	}
}

func TestTCPExec_NoAddrConfigured(t *testing.T) {
	t.Parallel()
	r := newRealOps(config{MwanAgentTCPAddr: ""}, defaultNetworkConfig())
	ctx := context.Background()
	_, err := r.tcpExec(ctx, "ping", "8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "no tcp addr configured") {
		t.Fatalf("got %v", err)
	}
}

func TestRealOpsSendEmail(t *testing.T) {
	t.Parallel()
	r := newRealOps(config{}, defaultNetworkConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = r.sendEmail(ctx, "a@b.com", "sub", "body")
}

func TestPingTargetOpsEdgeCases(t *testing.T) {
	t.Parallel()
	if pingTarget([]string{}) != "" {
		t.Fatal("empty args")
	}
	if got := pingTarget([]string{"-c"}); got != "-c" {
		t.Fatalf("got %q", got)
	}
	if pingIface([]string{"ping", "-I"}) != "" {
		t.Fatal("missing iface after -I")
	}
}

func TestGetenvUint32Valid(t *testing.T) {
	t.Setenv("MWAN_VSOCK_CID", "42")
	t.Setenv("MWAN_VSOCK_PORT", "50052")
	v, warn := getenvUint32("MWAN_VSOCK_CID", 1)
	if v != 42 || warn != "" {
		t.Fatalf("%d %q", v, warn)
	}
	v2, warn2 := getenvUint32("MWAN_VSOCK_PORT", 1)
	if v2 != 50052 || warn2 != "" {
		t.Fatalf("%d %q", v2, warn2)
	}
}

type errSlogHandler struct{}

func (errSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (errSlogHandler) Handle(context.Context, slog.Record) error {
	return errors.New("handler error")
}

func (e errSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return e }

func (e errSlogHandler) WithGroup(string) slog.Handler { return e }

func TestTeeHandlerHandleError(t *testing.T) {
	t.Parallel()
	th := newTeeHandler(
		errSlogHandler{},
		slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "x", 0)
	if err := th.Handle(context.Background(), rec); err == nil {
		t.Fatal("expected error from child")
	}
}

func TestRollback_WriteStateFails(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, "statedir")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockOps{}
	w := newTestWatchdog(t, m, func(c *config) {
		c.RollbackLockFile = filepath.Join(tmp, "lock.ok")
		c.RollbackStateFile = stateDir
		c.PostRollbackGraceSeconds = 0
	})
	w.rollback(context.Background(), 9, "snap")
}

func TestParseRollbackStateMalformedSkips(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "s")
	content := "notkeyvalue\nfoo=bar\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, done, snap, err := parseRollbackStateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if ds != "" || done || snap != "" {
		t.Fatalf("%q %v %q", ds, done, snap)
	}
}

func TestWatchdog_Run_PartialStillDegraded(t *testing.T) {
	t.Parallel()
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  false,
			"ping6:" + nc.PingTargetIPv6: true,
		},
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.MaxIterations = 3
		c.CheckIntervalDegraded = 0
		c.AlertCooldownSeconds = 0
	})
	w.run(context.Background())
}

func TestWatchdog_Run_TotalLossBeforeTimeout(t *testing.T) {
	t.Parallel()
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  false,
			"ping6:" + nc.PingTargetIPv6: false,
		},
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.MaxIterations = 2
		c.ConnectivityTimeoutSeconds = 600
		c.CheckIntervalDegraded = 0
	})
	w.run(context.Background())
}

// ---------------------------------------------------------------------------
// teeHandler.Enabled: all children disabled
// ---------------------------------------------------------------------------

func TestTeeHandlerEnabled_AllChildrenDisabled(t *testing.T) {
	t.Parallel()
	h1 := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})
	h2 := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})
	tee := newTeeHandler(h1, h2)
	ctx := context.Background()
	if tee.Enabled(ctx, slog.LevelDebug) {
		t.Fatal("Debug should be disabled when both handlers reject it")
	}
}

// ---------------------------------------------------------------------------
// fake qm on PATH (realOps vm* methods)
// ---------------------------------------------------------------------------

func writeFakeQM(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "qm")
	snapLine := "`-> pre-deploy-fake"
	script := `#!/usr/bin/env sh
case "$1" in
status)
  if [ "$2" = "999" ]; then exit 1; fi
  if [ "$2" = "998" ]; then echo "status: stopped"; exit 0; fi
  echo "status: running"
  ;;
stop)
  if [ "$2" = "999" ]; then exit 1; fi
  exit 0
  ;;
rollback)
  if [ "$2" = "999" ]; then exit 1; fi
  exit 0
  ;;
start)
  if [ "$2" = "999" ]; then exit 1; fi
  exit 0
  ;;
listsnapshot)
  if [ "$2" = "999" ]; then exit 1; fi
  echo snap-line
  echo '` + snapLine + `'
  ;;
*)
  exit 1
  ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func TestRealOps_QM_VMMethods(t *testing.T) {
	dir := writeFakeQM(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := newRealOps(config{}, defaultNetworkConfig())
	ctx := context.Background()

	t.Run("vmStatus running", func(t *testing.T) {
		ok, err := r.vmStatus(ctx, "113")
		if err != nil || !ok {
			t.Fatalf("got %v %v", ok, err)
		}
	})
	t.Run("vmStatus stopped", func(t *testing.T) {
		ok, err := r.vmStatus(ctx, "998")
		if err != nil || ok {
			t.Fatalf("got %v %v", ok, err)
		}
	})
	t.Run("vmStatus error", func(t *testing.T) {
		_, err := r.vmStatus(ctx, "999")
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("vmStop ok", func(t *testing.T) {
		if err := r.vmStop(ctx, "113"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("vmStop error", func(t *testing.T) {
		if err := r.vmStop(ctx, "999"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("vmRollback ok", func(t *testing.T) {
		if err := r.vmRollback(ctx, "113", "snap"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("vmRollback error", func(t *testing.T) {
		if err := r.vmRollback(ctx, "999", "snap"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("vmStart ok", func(t *testing.T) {
		if err := r.vmStart(ctx, "113"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("vmStart error", func(t *testing.T) {
		if err := r.vmStart(ctx, "999"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("vmSnapshots", func(t *testing.T) {
		b, err := r.vmSnapshots(ctx, "113")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "pre-deploy-fake") {
			t.Fatalf("got %q", b)
		}
	})
	t.Run("vmSnapshots error", func(t *testing.T) {
		_, err := r.vmSnapshots(ctx, "999")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// ---------------------------------------------------------------------------
// channelTracker (channels.go)
// ---------------------------------------------------------------------------

func TestChannelTracker_RecordSuccess(t *testing.T) {
	t.Parallel()
	tr := newChannelTracker()
	tr.recordSuccess(chanVsock)
	h := tr.channels[chanVsock]
	if !h.healthy || h.consecutiveFails != 0 || h.lastError != "" {
		t.Fatalf("got healthy=%v consecutiveFails=%d lastError=%q",
			h.healthy, h.consecutiveFails, h.lastError)
	}
}

func TestChannelTracker_RecordFailure(t *testing.T) {
	t.Parallel()
	tr := newChannelTracker()
	tr.recordFailure(chanVsock, errors.New("dial failed"))
	h := tr.channels[chanVsock]
	if h.healthy || h.consecutiveFails != 1 || h.lastError != "dial failed" {
		t.Fatalf("after first failure: healthy=%v consecutiveFails=%d lastError=%q",
			h.healthy, h.consecutiveFails, h.lastError)
	}
	tr.recordFailure(chanVsock, errors.New("second"))
	h = tr.channels[chanVsock]
	if h.consecutiveFails != 2 || h.lastError != "second" {
		t.Fatalf("after second failure: consecutiveFails=%d lastError=%q",
			h.consecutiveFails, h.lastError)
	}
	tr.recordSuccess(chanVsock)
	h = tr.channels[chanVsock]
	if h.consecutiveFails != 0 || !h.healthy {
		t.Fatalf("after success: consecutiveFails=%d healthy=%v",
			h.consecutiveFails, h.healthy)
	}
}

func TestChannelTracker_Summary(t *testing.T) {
	t.Parallel()
	tr := newChannelTracker()
	tr.recordSuccess(chanPVE)
	tr.recordFailure(chanTCP, errors.New("err"))
	s := tr.summary()
	for _, want := range []string{
		"pve_rest", "tcp_mgmt", "vsock", "OK", "FAIL",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary missing %q:\n%s", want, s)
		}
	}
}

func TestChannelTracker_LogAll(t *testing.T) {
	t.Parallel()
	tr := newChannelTracker()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tr.logAll(logger)
}

// ---------------------------------------------------------------------------
// vsockExec branches (no live vsock)
// ---------------------------------------------------------------------------

func TestRealOps_VsockExec_NoArgs(t *testing.T) {
	r := newRealOps(config{}, defaultNetworkConfig())
	_, err := r.vsockExec(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no args") {
		t.Fatalf("got %v", err)
	}
}

func TestRealOps_VsockExec_UnhandledCommand(t *testing.T) {
	r := newRealOps(config{}, defaultNetworkConfig())
	_, err := r.vsockExec(context.Background(), "unknown-cmd")
	if err == nil || !strings.Contains(err.Error(), "unhandled") {
		t.Fatalf("got %v", err)
	}
}

// grpcMWANAgentStub implements Ping and GetConfigState for bufconn vsockExec tests.
type grpcMWANAgentStub struct {
	mwanv1.UnimplementedMWANAgentServer
	pingErr     error
	pingSuccess bool
	deployEpoch int64
	cfgErr      error
}

func (g *grpcMWANAgentStub) Ping(
	_ context.Context, _ *mwanv1.PingRequest,
) (*mwanv1.PingResponse, error) {
	if g.pingErr != nil {
		return nil, g.pingErr
	}
	return &mwanv1.PingResponse{Success: g.pingSuccess}, nil
}

func (g *grpcMWANAgentStub) GetConfigState(
	_ context.Context, _ *mwanv1.GetConfigStateRequest,
) (*mwanv1.GetConfigStateResponse, error) {
	if g.cfgErr != nil {
		return nil, g.cfgErr
	}
	return &mwanv1.GetConfigStateResponse{LastDeployEpoch: g.deployEpoch}, nil
}

func grpcTestDialer(t *testing.T, impl mwanv1.MWANAgentServer) func(
	context.Context, string,
) (net.Conn, error) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	mwanv1.RegisterMWANAgentServer(srv, impl)
	go func() {
		if err := srv.Serve(lis); err != nil {
			t.Logf("grpc serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})
	return func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
}

func TestRealOps_VsockExec_BufconnPingSuccess(t *testing.T) {
	stub := &grpcMWANAgentStub{pingSuccess: true}
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testGrpcDialer = grpcTestDialer(t, stub)
	ctx := context.Background()
	res, err := r.vsockExec(ctx, "ping6", "-c", "2", "-W", "3", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit %d", res.ExitCode)
	}
}

func TestRealOps_VsockExec_BufconnPingFail(t *testing.T) {
	stub := &grpcMWANAgentStub{pingSuccess: false}
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testGrpcDialer = grpcTestDialer(t, stub)
	res, err := r.vsockExec(context.Background(), "ping", "-c", "2", "-W", "3", "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Fatal("expected nonzero exit")
	}
}

func TestRealOps_VsockExec_BufconnPingRPCError(t *testing.T) {
	stub := &grpcMWANAgentStub{pingErr: errors.New("rpc down")}
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testGrpcDialer = grpcTestDialer(t, stub)
	_, err := r.vsockExec(context.Background(), "ping6", "-c", "2", "-W", "3", "1.1.1.1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRealOps_VsockExec_BufconnCatDeploy(t *testing.T) {
	stub := &grpcMWANAgentStub{deployEpoch: 424242}
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testGrpcDialer = grpcTestDialer(t, stub)
	res, err := r.vsockExec(
		context.Background(), "cat", "/var/run/mwan-last-deploy",
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.Stdout != "424242" {
		t.Fatalf("got %+v", res)
	}
}

func TestRealOps_VsockExec_BufconnGetConfigStateError(t *testing.T) {
	stub := &grpcMWANAgentStub{cfgErr: errors.New("no state")}
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testGrpcDialer = grpcTestDialer(t, stub)
	_, err := r.vsockExec(
		context.Background(), "cat", "/var/run/mwan-last-deploy",
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRealOps_VsockExec_BufconnCatNotDeployPath(t *testing.T) {
	stub := &grpcMWANAgentStub{}
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testGrpcDialer = grpcTestDialer(t, stub)
	_, err := r.vsockExec(context.Background(), "cat", "/etc/hosts")
	if err == nil || !strings.Contains(err.Error(), "unhandled") {
		t.Fatalf("got %v", err)
	}
}

func TestRealOps_VsockExec_BufconnUnhandled(t *testing.T) {
	stub := &grpcMWANAgentStub{}
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testGrpcDialer = grpcTestDialer(t, stub)
	_, err := r.vsockExec(context.Background(), "echo", "hi")
	if err == nil || !strings.Contains(err.Error(), "unhandled") {
		t.Fatalf("got %v", err)
	}
}

func TestRealOps_VsockExec_BufconnNoArgs(t *testing.T) {
	stub := &grpcMWANAgentStub{}
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testGrpcDialer = grpcTestDialer(t, stub)
	_, err := r.vsockExec(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no args") {
		t.Fatalf("got %v", err)
	}
}

// ---------------------------------------------------------------------------
// pveExec + guestExec via httptest TLS server
// ---------------------------------------------------------------------------

func newPveTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestPveExec_Success(t *testing.T) {
	var statusCalls int32
	h := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"pid":42}}`)
		case strings.Contains(r.URL.Path, "/agent/exec-status"):
			n := atomic.AddInt32(&statusCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			if n == 1 {
				fmt.Fprint(w, `{"data":{"exited":0}}`)
				return
			}
			fmt.Fprint(w, `{"data":{"exited":1,"exitcode":0,"out-data":""}}`)
		default:
			http.NotFound(w, r)
		}
	}
	srv := newPveTestServer(t, h)
	r := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	ctx := context.Background()
	res, err := r.pveExec(ctx, "113", "echo", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.Stdout != "" {
		t.Fatalf("code=%d out=%q", res.ExitCode, res.Stdout)
	}
}

func TestGuestExec_VsockOverrideSuccess(t *testing.T) {
	r := newRealOps(config{}, defaultNetworkConfig())
	r.testVsockOverride = func(
		_ context.Context, _ ...string,
	) (guestExecResult, error) {
		return guestExecResult{ExitCode: 0, Stdout: "vsock-ok"}, nil
	}
	res, err := r.guestExec(context.Background(), "113", "ping", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.Stdout != "vsock-ok" {
		t.Fatalf("got %+v", res)
	}
}

func TestGuestExec_VsockOverrideErrUsesPVE(t *testing.T) {
	var statusCalls int32
	h := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"pid":1}}`)
		case strings.Contains(r.URL.Path, "/agent/exec-status"):
			n := atomic.AddInt32(&statusCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			if n == 1 {
				fmt.Fprint(w, `{"data":{"exited":0}}`)
				return
			}
			fmt.Fprint(w, `{"data":{"exited":1,"exitcode":0,"out-data":""}}`)
		default:
			http.NotFound(w, r)
		}
	}
	srv := newPveTestServer(t, h)
	ro := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	ro.testVsockOverride = func(
		_ context.Context, _ ...string,
	) (guestExecResult, error) {
		return guestExecResult{ExitCode: 1}, errors.New("vsock failed")
	}
	res, err := ro.guestExec(context.Background(), "113", "echo", "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit %d", res.ExitCode)
	}
}

func TestPveExec_GuestExecFallback(t *testing.T) {
	var statusCalls int32
	h := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"pid":1}}`)
		case strings.Contains(r.URL.Path, "/agent/exec-status"):
			n := atomic.AddInt32(&statusCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			if n == 1 {
				fmt.Fprint(w, `{"data":{"exited":0}}`)
				return
			}
			fmt.Fprint(w, `{"data":{"exited":1,"exitcode":0,"out-data":""}}`)
		default:
			http.NotFound(w, r)
		}
	}
	srv := newPveTestServer(t, h)
	ro := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	res, err := ro.guestExec(context.Background(), "113", "echo", "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit %d", res.ExitCode)
	}
}

func TestGuestExec_ThreeChannelFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	failDial := func(context.Context, string) (net.Conn, error) {
		return nil, net.ErrClosed
	}

	t.Run("all three fail", func(t *testing.T) {
		r := newRealOps(config{MwanAgentTCPAddr: "127.0.0.1:1"}, defaultNetworkConfig())
		r.testGrpcDialer = failDial
		r.testTCPDialer = failDial
		_, err := r.guestExec(ctx, "113", "ping", "-c", "2", "-W", "3", "8.8.8.8")
		if err == nil {
			t.Fatal("expected error when vsock, tcp, and pve all fail")
		}
		if !strings.Contains(err.Error(), "PVE_TOKEN") {
			t.Fatalf("expected pve not configured error, got %v", err)
		}
		vs := r.tracker.channels[chanVsock]
		tcp := r.tracker.channels[chanTCP]
		pve := r.tracker.channels[chanPVE]
		if vs.healthy || tcp.healthy || pve.healthy {
			t.Fatalf("expected all channels unhealthy: vsock=%v tcp=%v pve=%v",
				vs.healthy, tcp.healthy, pve.healthy)
		}
		if vs.consecutiveFails != 1 || tcp.consecutiveFails != 1 || pve.consecutiveFails != 1 {
			t.Fatalf("consecutiveFails vsock=%d tcp=%d pve=%d",
				vs.consecutiveFails, tcp.consecutiveFails, pve.consecutiveFails)
		}
	})

	t.Run("tcp succeeds after vsock fails", func(t *testing.T) {
		stub := &grpcMWANAgentStub{pingSuccess: true}
		r := newRealOps(
			config{MwanAgentTCPAddr: "127.0.0.1:9"},
			defaultNetworkConfig(),
		)
		r.testGrpcDialer = failDial
		r.testTCPDialer = grpcTestDialer(t, stub)
		res, err := r.guestExec(
			ctx, "113", "ping", "-c", "2", "-W", "3", "8.8.8.8",
		)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("want exit 0, got %d", res.ExitCode)
		}
		if r.tracker.channels[chanVsock].healthy {
			t.Fatal("vsock should be recorded as failed")
		}
		if !r.tracker.channels[chanTCP].healthy {
			t.Fatal("tcp should be recorded as success")
		}
	})
}

func TestPveExec_HTTPErrorOnExec(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec") {
			http.Error(w, "nope", http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	}
	srv := newPveTestServer(t, h)
	r := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	_, err := r.pveExec(context.Background(), "113", "true")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPveExec_JSONDecodeErrorOnExec(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `not-json`)
			return
		}
		http.NotFound(w, r)
	}
	srv := newPveTestServer(t, h)
	r := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	_, err := r.pveExec(context.Background(), "113", "true")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPveExec_ExecStatusHTTPError(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"pid":7}}`)
		case strings.Contains(r.URL.Path, "/agent/exec-status"):
			http.Error(w, "fail", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}
	srv := newPveTestServer(t, h)
	r := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	_, err := r.pveExec(context.Background(), "113", "true")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPveExec_ExecStatusJSONError(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"pid":7}}`)
		case strings.Contains(r.URL.Path, "/agent/exec-status"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `<<<`)
		default:
			http.NotFound(w, r)
		}
	}
	srv := newPveTestServer(t, h)
	r := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	_, err := r.pveExec(context.Background(), "113", "true")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPveExec_ContextCancelledInStatusLoop(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"pid":7}}`)
		case strings.Contains(r.URL.Path, "/agent/exec-status"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"exited":0}}`)
		default:
			http.NotFound(w, r)
		}
	}
	srv := newPveTestServer(t, h)
	r := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.pveExec(ctx, "113", "true")
	if err == nil {
		t.Fatal("expected cancel error")
	}
}

func TestPveExec_MissingPID(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/agent/exec") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"pid":0}}`)
			return
		}
		http.NotFound(w, r)
	}
	srv := newPveTestServer(t, h)
	r := newRealOps(config{
		PVETokenID: "id",
		PVESecret:  "sec",
		PVEBaseURL: srv.URL + "/api2/json",
		PVENode:    "n1",
	}, defaultNetworkConfig())
	_, err := r.pveExec(context.Background(), "113", "true")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// rollback exitFn + remove lock error + recoverInterrupted remove error
// ---------------------------------------------------------------------------

func TestRollback_ExitFnAfterSignal(t *testing.T) {
	tmp := t.TempDir()
	m := &mockOps{}
	w := newTestWatchdog(t, m, func(c *config) {
		c.RollbackLockFile = filepath.Join(tmp, "rollback.lock")
		c.RollbackStateFile = filepath.Join(tmp, "rollback.state")
		c.PostRollbackGraceSeconds = 0
	})
	var exitArg int
	var exitCalled bool
	w.exitFn = func(code int) {
		exitCalled = true
		exitArg = code
	}
	w.coord.onSignalDuringRollback()
	w.rollback(context.Background(), 42, "snap-a")
	if !exitCalled || exitArg != 0 {
		t.Fatalf("exitCalled=%v arg=%d", exitCalled, exitArg)
	}
}

func TestRecoverInterrupted_RemoveLockError_VMRunning(t *testing.T) {
	tmp := t.TempDir()
	lockPath := filepath.Join(tmp, "lock")
	if err := os.WriteFile(lockPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmp, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmp, 0o700) })
	m := &mockOps{vmRunning: true}
	w := newTestWatchdog(t, m, func(c *config) {
		c.RollbackLockFile = lockPath
		c.PostRollbackGraceSeconds = 0
	})
	w.recoverInterrupted(context.Background())
}

func TestRecoverInterrupted_RemoveLockError_AfterStartFail(t *testing.T) {
	tmp := t.TempDir()
	lockPath := filepath.Join(tmp, "lock2")
	if err := os.WriteFile(lockPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmp, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmp, 0o700) })
	m := &mockOps{vmRunning: false, vmStartErr: errors.New("start")}
	w := newTestWatchdog(t, m, func(c *config) {
		c.RollbackLockFile = lockPath
		c.PostRollbackGraceSeconds = 0
	})
	w.recoverInterrupted(context.Background())
}

// ---------------------------------------------------------------------------
// run(): heartbeat + context cancel during sleeps
// ---------------------------------------------------------------------------

func TestWatchdog_Run_HeartbeatLog(t *testing.T) {
	t.Parallel()
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  true,
			"ping6:" + nc.PingTargetIPv6: true,
		},
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.MaxIterations = 4
		c.CheckIntervalHealthy = 0
	})
	w.nc = nc
	w.testHeartbeatInterval = 1
	w.run(context.Background())
}

func TestWatchdog_Run_ContextCancelledDuringHealthySleep(t *testing.T) {
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  true,
			"ping6:" + nc.PingTargetIPv6: true,
		},
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.CheckIntervalHealthy = 100 * time.Millisecond
	})
	w.nc = nc
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	w.run(ctx)
}

func TestWatchdog_Run_ContextCancelledDuringVMStoppedSleep(t *testing.T) {
	m := &mockOps{vmRunning: false}
	w := newTestWatchdog(t, m, func(c *config) {
		c.CheckIntervalDegraded = 50 * time.Millisecond
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	w.run(ctx)
}

func TestWatchdog_Run_ContextCancelledDuringPartialSleep(t *testing.T) {
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  false,
			"ping6:" + nc.PingTargetIPv6: true,
		},
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.CheckIntervalDegraded = 50 * time.Millisecond
		c.AlertCooldownSeconds = 0
	})
	w.nc = nc
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	w.run(ctx)
}

func TestWatchdog_Run_ContextCancelledDuringTotalLossSleep(t *testing.T) {
	nc := testNC()
	m := &mockOps{
		vmRunning: true,
		pingResults: map[string]bool{
			"ping:" + nc.PingTargetIPv4:  false,
			"ping6:" + nc.PingTargetIPv6: false,
		},
	}
	w := newTestWatchdog(t, m, func(c *config) {
		c.ConnectivityTimeoutSeconds = 600
		c.CheckIntervalDegraded = 50 * time.Millisecond
	})
	w.nc = nc
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	w.run(ctx)
}
