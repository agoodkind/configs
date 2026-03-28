package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
		if cfg.ConnectivityTimeoutSeconds != 60 {
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
	if !emailSubjectContains(m.emailsSent, "Auto-Rollback") {
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
		if strings.Contains(e.Subject, "Auto-Rollback") {
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
		if strings.Contains(e.Subject, "MWAN Connectivity Alert") &&
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
	if len(m.emailsSent) != 0 || len(m.vmStopCalls) != 0 {
		t.Fatalf("unexpected emails or stop: emails=%d stop=%d",
			len(m.emailsSent), len(m.vmStopCalls))
	}
	if m.pingCallCount != 0 || m.guestExecCallCount != 0 {
		t.Fatalf("VM stopped should skip host/guest probes: ping=%d guest=%d",
			m.pingCallCount, m.guestExecCallCount)
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
				if !emailSubjectContains(inner.emailsSent, "Auto-Rollback") {
					t.Fatalf("emails %+v", inner.emailsSent)
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
					if e.Subject == "MWAN Connectivity Alert" &&
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
		if !emailSubjectContains(m.emailsSent, "MWAN Connectivity Alert") {
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
			!strings.Contains(m.emailsSent[0].Body, "no pre-deploy") {
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
		vmRunning:   true,
		snapshotsOut: []byte("x"),
		pingResults: map[string]bool{"ping:1.1.1.1": true},
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
		inner: inner,
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
	if len(inner.vmStopCalls) != 1 {
		t.Fatal("stop")
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
		inner:  inner,
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		nc:     testNC(),
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
