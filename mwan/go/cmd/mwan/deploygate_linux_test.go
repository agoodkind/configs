//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"
)

const (
	testOldBootID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	testNewBootID = "11111111-2222-3333-4444-555555555555"
)

// fakeClock advances only when the gate sleeps, so the polling loops run
// deterministically without wall-clock time.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

func newTestDeps(out *strings.Builder, clock *fakeClock) deployGateDeps {
	return deployGateDeps{
		out:   out,
		now:   clock.Now,
		sleep: clock.Sleep,
	}
}

func TestWaitRebootDetectsBootIDChange(t *testing.T) {
	var out strings.Builder
	clock := &fakeClock{now: time.Unix(1000, 0)}
	deps := newTestDeps(&out, clock)
	reads := 0
	deps.readBootID = func(_ context.Context, _ int) (string, error) {
		reads++
		if reads < 3 {
			return testOldBootID, nil
		}
		return testNewBootID, nil
	}

	code := waitReboot(context.Background(), deps, 113, testOldBootID, time.Minute)

	if code != exitDeployGateOK {
		t.Fatalf("exit code = %d, want %d\noutput: %s", code, exitDeployGateOK, out.String())
	}
	if !strings.Contains(out.String(), testNewBootID) {
		t.Fatalf("output does not name the new boot_id: %s", out.String())
	}
}

func TestWaitRebootAgentSilentUntilNewBootID(t *testing.T) {
	var out strings.Builder
	clock := &fakeClock{now: time.Unix(1000, 0)}
	deps := newTestDeps(&out, clock)
	reads := 0
	deps.readBootID = func(_ context.Context, _ int) (string, error) {
		reads++
		if reads == 1 {
			return testOldBootID, nil
		}
		if reads < 5 {
			return "", errors.New("guest agent not running")
		}
		return testNewBootID, nil
	}

	code := waitReboot(context.Background(), deps, 113, testOldBootID, time.Minute)

	if code != exitDeployGateOK {
		t.Fatalf("exit code = %d, want %d\noutput: %s", code, exitDeployGateOK, out.String())
	}
}

func TestWaitRebootNeverFiredFailsDefinitively(t *testing.T) {
	var out strings.Builder
	clock := &fakeClock{now: time.Unix(1000, 0)}
	deps := newTestDeps(&out, clock)
	deps.readBootID = func(_ context.Context, _ int) (string, error) {
		return testOldBootID, nil
	}

	code := waitReboot(context.Background(), deps, 113, testOldBootID, 10*time.Second)

	if code != exitDeployGateFailed {
		t.Fatalf("exit code = %d, want %d\noutput: %s", code, exitDeployGateFailed, out.String())
	}
	if !strings.Contains(out.String(), "never fired") {
		t.Fatalf("output does not state the reboot never fired: %s", out.String())
	}
}

func TestWaitRebootUnobservableDefersToEgressGate(t *testing.T) {
	var out strings.Builder
	clock := &fakeClock{now: time.Unix(1000, 0)}
	deps := newTestDeps(&out, clock)
	deps.readBootID = func(_ context.Context, _ int) (string, error) {
		return "", errors.New("guest agent not running")
	}

	code := waitReboot(context.Background(), deps, 113, testOldBootID, 10*time.Second)

	if code != exitDeployGateUnobservable {
		t.Fatalf("exit code = %d, want %d\noutput: %s",
			code, exitDeployGateUnobservable, out.String())
	}
	if !strings.Contains(out.String(), "deferring") {
		t.Fatalf("output does not defer the verdict: %s", out.String())
	}
}

func TestWaitRebootRecoveryOnFinalRead(t *testing.T) {
	var out strings.Builder
	clock := &fakeClock{now: time.Unix(1000, 0)}
	deps := newTestDeps(&out, clock)
	reads := 0
	deps.readBootID = func(_ context.Context, _ int) (string, error) {
		reads++
		if reads <= 4 {
			return "", errors.New("guest agent not running")
		}
		// Only the post-deadline final read succeeds, with a new boot_id.
		return testNewBootID, nil
	}

	code := waitReboot(context.Background(), deps, 113, testOldBootID, 10*time.Second)

	if code != exitDeployGateOK {
		t.Fatalf("exit code = %d, want %d\noutput: %s", code, exitDeployGateOK, out.String())
	}
}

func TestWaitEgressV6Decides(t *testing.T) {
	var out strings.Builder
	clock := &fakeClock{now: time.Unix(1000, 0)}
	deps := newTestDeps(&out, clock)
	probes := 0
	deps.ping6 = func(context.Context, netip.Addr, time.Duration) (time.Duration, error) {
		probes++
		if probes < 3 {
			return 0, errors.New("no route to host")
		}
		return time.Millisecond, nil
	}
	deps.ping4 = func(
		context.Context, string, netip.Addr, time.Duration,
	) (time.Duration, error) {
		return 0, errors.New("still down")
	}

	code := waitEgress(context.Background(), deps, time.Minute)

	if code != exitDeployGateOK {
		t.Fatalf("exit code = %d, want %d\noutput: %s", code, exitDeployGateOK, out.String())
	}
	if !strings.Contains(out.String(), "ipv6=yes ipv4=no") {
		t.Fatalf("output does not report both families: %s", out.String())
	}
}

func TestWaitEgressTimesOut(t *testing.T) {
	var out strings.Builder
	clock := &fakeClock{now: time.Unix(1000, 0)}
	deps := newTestDeps(&out, clock)
	deps.ping6 = func(context.Context, netip.Addr, time.Duration) (time.Duration, error) {
		return 0, errors.New("no route to host")
	}
	deps.ping4 = func(
		context.Context, string, netip.Addr, time.Duration,
	) (time.Duration, error) {
		return time.Millisecond, nil
	}

	code := waitEgress(context.Background(), deps, 5*time.Second)

	if code != exitDeployGateFailed {
		t.Fatalf("exit code = %d, want %d\noutput: %s", code, exitDeployGateFailed, out.String())
	}
}

func TestCheckEgressEitherFamilySuffices(t *testing.T) {
	var out strings.Builder
	clock := &fakeClock{now: time.Unix(1000, 0)}
	deps := newTestDeps(&out, clock)
	deps.ping6 = func(context.Context, netip.Addr, time.Duration) (time.Duration, error) {
		return 0, errors.New("v6 down")
	}
	deps.ping4 = func(
		context.Context, string, netip.Addr, time.Duration,
	) (time.Duration, error) {
		return time.Millisecond, nil
	}

	if code := checkEgress(context.Background(), deps); code != exitDeployGateOK {
		t.Fatalf("exit code = %d, want %d\noutput: %s", code, exitDeployGateOK, out.String())
	}

	deps.ping4 = func(
		context.Context, string, netip.Addr, time.Duration,
	) (time.Duration, error) {
		return 0, errors.New("v4 down")
	}
	if code := checkEgress(context.Background(), deps); code != exitDeployGateFailed {
		t.Fatalf("exit code = %d, want %d\noutput: %s", code, exitDeployGateFailed, out.String())
	}
}

func TestUnmarshalGuestBootID(t *testing.T) {
	valid := fmt.Sprintf(`{"exitcode": 0, "out-data": "%s\n"}`, testOldBootID)
	bootID, err := unmarshalGuestBootID([]byte(valid))
	if err != nil {
		t.Fatalf("unmarshalGuestBootID(valid): %v", err)
	}
	if bootID != testOldBootID {
		t.Fatalf("bootID = %q, want %q", bootID, testOldBootID)
	}

	cases := map[string]string{
		"nonzero exit": `{"exitcode": 1, "out-data": "boom"}`,
		"not a uuid":   `{"exitcode": 0, "out-data": "hello"}`,
		"empty out":    `{"exitcode": 0, "out-data": ""}`,
		"malformed":    `QEMU guest agent is not running`,
		"truncated":    `{"exitcode": 0, "out-data": "aaaaaaaa-bbbb"}`,
		"upper hex":    `{"exitcode": 0, "out-data": "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"}`,
	}
	for name, raw := range cases {
		if _, err := unmarshalGuestBootID([]byte(raw)); err == nil {
			t.Fatalf("unmarshalGuestBootID(%s) accepted %q", name, raw)
		}
	}
}

func TestRunDeployGateUsageErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"bogus-mode"},
		{"wait-reboot", "113", "not-a-uuid", "180"},
		{"wait-reboot", "113", testOldBootID, "0"},
		{"wait-reboot", "113; rm", testOldBootID, "180"},
		{"wait-reboot", "113", testOldBootID},
		{"wait-egress"},
		{"check-egress", "extra"},
	}
	for _, args := range cases {
		if code := runDeployGate(args); code != exitDeployGateUsage {
			t.Fatalf("runDeployGate(%v) = %d, want %d", args, code, exitDeployGateUsage)
		}
	}
}
