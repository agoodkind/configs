//go:build linux

package health

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/notify"
)

func TestAdvanceHealthAppliesConsecutiveThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		cycles            []bool
		failureThreshold  int
		recoveryThreshold int
		want              []State
	}{
		{
			name:              "unknown becomes healthy after consecutive successes",
			cycles:            []bool{true, true},
			failureThreshold:  2,
			recoveryThreshold: 2,
			want:              []State{StateUnknown, StateHealthy},
		},
		{
			name:              "unknown becomes unhealthy after consecutive failures",
			cycles:            []bool{false, false},
			failureThreshold:  2,
			recoveryThreshold: 2,
			want:              []State{StateUnknown, StateUnhealthy},
		},
		{
			name:              "opposite result resets the active counter",
			cycles:            []bool{true, false, true, true, false, false},
			failureThreshold:  2,
			recoveryThreshold: 2,
			want: []State{
				StateUnknown,
				StateUnknown,
				StateUnknown,
				StateHealthy,
				StateHealthy,
				StateUnhealthy,
			},
		},
		{
			name:              "healthy waits for recovery threshold after failure",
			cycles:            []bool{false, false, true, true, true},
			failureThreshold:  2,
			recoveryThreshold: 3,
			want: []State{
				StateUnknown,
				StateUnhealthy,
				StateUnhealthy,
				StateUnhealthy,
				StateHealthy,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			status := wanStatus{State: StateUnknown}
			got := make([]State, 0, len(test.cycles))
			for _, passed := range test.cycles {
				status, _ = advanceHealth(
					status,
					passed,
					test.failureThreshold,
					test.recoveryThreshold,
				)
				got = append(got, status.State)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("states = %v, want %v", got, test.want)
			}
		})
	}
}

func TestNewWithoutConfigDefaultsToShadow(t *testing.T) {
	t.Parallel()

	untypedModule, err := New(nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	module, ok := untypedModule.(*Module)
	if !ok {
		t.Fatalf("New returned %T, want *Module", untypedModule)
	}
	if !module.cfg.ShadowMode {
		t.Fatal("New(nil) must default ShadowMode to true")
	}
}

func TestProbeWANVerdictIsV6OrV4AndAlwaysProbesBoth(t *testing.T) {
	t.Parallel()

	// The verdict matches health-check.sh: healthy when IPv6 meets the
	// threshold (preferred) or IPv4 meets it (fallback). Both families are
	// always probed regardless of outcome, so a v4 flap never fails a
	// healthy-v6 WAN and a v6 outage can still fall back to v4.
	tests := []struct {
		name     string
		v6Passes bool
		v4Passes bool
		wantPass bool
	}{
		{
			name:     "IPv6 up keeps the WAN healthy when IPv4 is down",
			v6Passes: true,
			v4Passes: false,
			wantPass: true,
		},
		{
			name:     "IPv6 down falls back to IPv4",
			v6Passes: false,
			v4Passes: true,
			wantPass: true,
		},
		{
			name:     "both families down is unhealthy",
			v6Passes: false,
			v4Passes: false,
			wantPass: false,
		},
		{
			name:     "both families up is healthy",
			v6Passes: true,
			v4Passes: true,
			wantPass: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var calls []string
			module := testProbeModule(
				func(
					_ context.Context,
					_ string,
					_ netip.Addr,
					_ time.Duration,
				) (time.Duration, error) {
					calls = append(calls, "v6")
					if test.v6Passes {
						return time.Millisecond, nil
					}
					return 0, errors.New("IPv6 unavailable")
				},
				func(
					_ context.Context,
					_ string,
					_ netip.Addr,
					_ time.Duration,
				) (time.Duration, error) {
					calls = append(calls, "v4")
					if test.v4Passes {
						return time.Millisecond, nil
					}
					return 0, errors.New("IPv4 unavailable")
				},
			)

			result := module.probeWAN(
				context.Background(),
				module.cfg.WANs[0],
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if result.Passed != test.wantPass {
				t.Fatalf("Passed = %t, want %t", result.Passed, test.wantPass)
			}
			wantCalls := []string{"v6", "v6", "v4", "v4"}
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("probe calls = %v, want %v", calls, wantCalls)
			}
		})
	}
}

func TestProbeWANHTTPSucceedsWhenBothPingFamiliesFail(t *testing.T) {
	t.Parallel()

	pingFail := func(
		_ context.Context,
		_ string,
		_ netip.Addr,
		_ time.Duration,
	) (time.Duration, error) {
		return 0, errors.New("ping unavailable")
	}
	module := testProbeModule(pingFail, pingFail)
	module.cfg.HTTPURLs = []string{"http://example.test/probe"}
	module.probeHTTP = func(
		_ context.Context,
		_ string,
		_ string,
		_ time.Duration,
	) (int, error) {
		return 200, nil
	}

	result := module.probeWAN(
		context.Background(),
		module.cfg.WANs[0],
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if !result.Passed {
		t.Fatal("a successful HTTP probe must keep the WAN healthy when both ping families fail")
	}
}

func TestProbeTargetsUsesEveryConfiguredPing(t *testing.T) {
	t.Parallel()

	callCount := 0
	module := testProbeModule(
		func(
			_ context.Context,
			_ string,
			_ netip.Addr,
			_ time.Duration,
		) (time.Duration, error) {
			callCount++
			return time.Millisecond, nil
		},
		func(
			_ context.Context,
			_ string,
			_ netip.Addr,
			_ time.Duration,
		) (time.Duration, error) {
			return time.Millisecond, nil
		},
	)
	module.cfg.TargetsV6 = module.cfg.TargetsV6[:1]
	module.cfg.PingCount = 3

	successes := module.probeTargets(
		context.Background(),
		module.cfg.WANs[0],
		module.cfg.TargetsV6,
		module.probeV6,
	)
	if successes != 1 {
		t.Fatalf("successes = %d, want 1", successes)
	}
	if callCount != 3 {
		t.Fatalf("probe call count = %d, want 3", callCount)
	}
}

func TestWriteStateFilesUsesShellFormatAndShadowPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		shadowMode bool
		suffix     string
	}{
		{name: "authoritative paths", shadowMode: false, suffix: ""},
		{name: "shadow paths", shadowMode: true, suffix: ".shadow"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			stateFile := filepath.Join(tempDir, "run", "mwan-health.state")
			persistStateFile := filepath.Join(tempDir, "lib", "health-state")
			module := &Module{
				cfg: Config{
					ShadowMode:       test.shadowMode,
					StateFile:        stateFile,
					PersistStateFile: persistStateFile,
					WANs: []WAN{
						{WANRef: ifmgr.WANRef{Name: "att", Iface: "att0"}},
						{WANRef: ifmgr.WANRef{Name: "webpass", Iface: "webpass0"}},
					},
				},
				statuses: map[string]wanStatus{
					"att":     {State: StateHealthy},
					"webpass": {State: StateUnhealthy},
				},
			}

			if err := module.writeStateFiles(
				context.Background(),
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				module.statuses,
			); err != nil {
				t.Fatalf("writeStateFiles: %v", err)
			}

			wantContents := "att:healthy\nwebpass:unhealthy\n"
			for _, path := range []string{
				stateFile + test.suffix,
				persistStateFile + test.suffix,
			} {
				contents, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("ReadFile(%q): %v", path, err)
				}
				if string(contents) != wantContents {
					t.Fatalf("%s contents = %q, want %q", path, contents, wantContents)
				}
			}

			unselectedPath := stateFile
			if !test.shadowMode {
				unselectedPath += ".shadow"
			}
			if _, err := os.Stat(unselectedPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unselected state path %q exists or returned err=%v", unselectedPath, err)
			}
		})
	}
}

func TestWriteStateFilesToleratesPersistFailure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	blocker := filepath.Join(tempDir, "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}
	statePath := filepath.Join(tempDir, "run", "mwan-health.state")
	module := &Module{
		cfg: Config{
			ShadowMode:       false,
			StateFile:        statePath,
			PersistStateFile: filepath.Join(blocker, "health-state"),
			WANs:             []WAN{{WANRef: ifmgr.WANRef{Name: "att", Iface: "att0"}}},
		},
		statuses: map[string]wanStatus{"att": {State: StateHealthy}},
	}

	// The persist mirror lives under an unwritable path, mirroring the ifmgr
	// sandbox where /var/lib is read-only. The runtime write must still succeed
	// and the call must not error.
	if err := module.writeStateFiles(
		context.Background(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		module.statuses,
	); err != nil {
		t.Fatalf("writeStateFiles must tolerate a persist failure: %v", err)
	}
	contents, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("runtime state not written: %v", err)
	}
	if string(contents) != "att:healthy\n" {
		t.Fatalf("runtime state = %q, want %q", contents, "att:healthy\n")
	}
}

func TestInitFailsWhenRuntimeStateUnwritable(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	blocker := filepath.Join(tempDir, "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}
	module := &Module{
		cfg: Config{
			ShadowMode:       false,
			StateFile:        filepath.Join(blocker, "mwan-health.state"),
			PersistStateFile: filepath.Join(tempDir, "health-state"),
			TargetsV6: []netip.Addr{
				netip.MustParseAddr("2001:db8::1"),
				netip.MustParseAddr("2001:db8::2"),
			},
			TargetsV4: []netip.Addr{
				netip.MustParseAddr("192.0.2.1"),
				netip.MustParseAddr("192.0.2.2"),
			},
			Timeout:           time.Second,
			Interval:          10 * time.Second,
			PingCount:         1,
			SuccessThreshold:  1,
			FailureThreshold:  1,
			RecoveryThreshold: 1,
			WANs:              []WAN{{WANRef: ifmgr.WANRef{Name: "att", Iface: "att0"}}},
		},
	}
	env := &ifmgr.Env{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	// The runtime state file lives under an unwritable path. The runtime output
	// is required, so Init must fail rather than start the loop with no state
	// file. A persist-only failure is tolerated separately (see the persist
	// test), so this asserts the runtime path stays load-bearing.
	if err := module.Init(context.Background(), env); err == nil {
		t.Fatal("Init must fail when the runtime state file cannot be written")
	}
}

func TestRunCycleDoesNotCommitStateWhenPublicationFails(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	blockedParent := filepath.Join(tempDir, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}
	module := testProbeModule(
		func(
			_ context.Context,
			_ string,
			_ netip.Addr,
			_ time.Duration,
		) (time.Duration, error) {
			return 0, errors.New("IPv6 unavailable")
		},
		func(
			_ context.Context,
			_ string,
			_ netip.Addr,
			_ time.Duration,
		) (time.Duration, error) {
			return 0, errors.New("IPv4 unavailable")
		},
	)
	module.cfg.StateFile = filepath.Join(blockedParent, "mwan-health.state")
	module.cfg.PersistStateFile = filepath.Join(tempDir, "health-state")
	module.cfg.FailureThreshold = 1
	module.cfg.RecoveryThreshold = 1
	module.statuses = map[string]wanStatus{
		"att": {State: StateHealthy},
	}

	err := module.runCycle(
		context.Background(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err == nil {
		t.Fatal("runCycle must fail when the runtime state cannot be written")
	}
	if got := module.statuses["att"].State; got != StateHealthy {
		t.Fatalf("state after failed publication = %s, want %s", got, StateHealthy)
	}
}

func TestReconcileRunsOnlyTheStartupProbe(t *testing.T) {
	t.Parallel()

	callCount := 0
	probe := func(
		_ context.Context,
		_ string,
		_ netip.Addr,
		_ time.Duration,
	) (time.Duration, error) {
		callCount++
		return time.Millisecond, nil
	}
	module := testProbeModule(probe, probe)
	tempDir := t.TempDir()
	module.cfg.StateFile = filepath.Join(tempDir, "mwan-health.state")
	module.cfg.PersistStateFile = filepath.Join(tempDir, "health-state")
	module.cfg.FailureThreshold = 1
	module.cfg.RecoveryThreshold = 1
	module.statuses = map[string]wanStatus{
		"att": {State: StateUnknown},
	}
	module.reconcilePending = true
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := module.Reconcile(context.Background(), log); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if err := module.Reconcile(context.Background(), log); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if callCount != 4 {
		t.Fatalf("probe call count = %d, want 4 from one dual-family cycle", callCount)
	}
}

func TestEmitTransitionsUsesInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	notifier := &recordingNotifier{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	module := &Module{
		BaseModule: ifmgr.NewBaseModule(moduleName),
		cfg: Config{
			ShadowMode: false,
		},
		clock: fixedClock{now: now},
	}
	module.InitBase(&ifmgr.Env{
		Iface:   "",
		Sysctl:  nil,
		Log:     log,
		Alerts:  ifmgr.WrapNotifier(notifier),
		Monitor: nil,
		DHCP:    nil,
		RA:      nil,
	}, "module", moduleName)

	module.emitTransitions(context.Background(), log, []transition{
		{
			WAN:  WAN{WANRef: ifmgr.WANRef{Name: "att", Iface: "att0"}},
			From: StateHealthy,
			To:   StateUnhealthy,
		},
	})

	if len(notifier.events) != 1 {
		t.Fatalf("Notify event count = %d, want 1", len(notifier.events))
	}
	if notifier.events[0].Now != now {
		t.Fatalf("Notify event time = %s, want %s", notifier.events[0].Now, now)
	}
}

func TestEmitTransitionAlertsFromUnknownUnhealthy(t *testing.T) {
	t.Parallel()

	notifier := &recordingNotifier{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	module := &Module{
		BaseModule: ifmgr.NewBaseModule(moduleName),
		cfg:        Config{ShadowMode: false},
		clock:      fixedClock{now: time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)},
	}
	module.InitBase(&ifmgr.Env{Log: log, Alerts: ifmgr.WrapNotifier(notifier)}, "module", moduleName)

	// A WAN broken from startup goes unknown -> unhealthy and must alert.
	module.emitTransitions(context.Background(), log, []transition{
		{WAN: WAN{WANRef: ifmgr.WANRef{Name: "att", Iface: "att0"}}, From: StateUnknown, To: StateUnhealthy},
	})
	if len(notifier.events) != 1 {
		t.Fatalf("unknown->unhealthy Notify count = %d, want 1", len(notifier.events))
	}

	// A WAN that comes up healthy from unknown has no prior alert to resolve.
	module.emitTransitions(context.Background(), log, []transition{
		{WAN: WAN{WANRef: ifmgr.WANRef{Name: "webpass", Iface: "webpass0"}}, From: StateUnknown, To: StateHealthy},
	})
	if len(notifier.events) != 1 {
		t.Fatalf("unknown->healthy must not emit a Notify; count = %d, want 1", len(notifier.events))
	}
}

func testProbeModule(probeV6 pingFunc, probeV4 pingFunc) *Module {
	return &Module{
		cfg: Config{
			TargetsV6: []netip.Addr{
				netip.MustParseAddr("2001:db8::1"),
				netip.MustParseAddr("2001:db8::2"),
			},
			TargetsV4: []netip.Addr{
				netip.MustParseAddr("192.0.2.1"),
				netip.MustParseAddr("192.0.2.2"),
			},
			Timeout:          time.Second,
			PingCount:        1,
			SuccessThreshold: 2,
			WANs: []WAN{
				{WANRef: ifmgr.WANRef{Name: "att", Iface: "att0"}},
			},
		},
		probeV6: probeV6,
		probeV4: probeV4,
	}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type recordingNotifier struct {
	events []notify.Event
}

func (n *recordingNotifier) Notify(_ context.Context, event notify.Event) {
	n.events = append(n.events, event)
}

func (n *recordingNotifier) Resolve(
	_ context.Context,
	_,
	_,
	_ string,
	_ ...slog.Attr,
) {
}

func (n *recordingNotifier) Active(_, _ string) bool {
	return len(n.events) > 0
}
