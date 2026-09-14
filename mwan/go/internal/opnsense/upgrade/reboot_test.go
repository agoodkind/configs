package upgrade

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// steppingClock advances by step on every read, so a wait's deadline
// passes after a known number of reads without real time passing.
type steppingClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func (c *steppingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.now
	c.now = c.now.Add(c.step)
	return current
}

func testRebootWait() rebootWait {
	return rebootWait{timeout: DefaultPostRebootTimeout, interval: time.Millisecond, probeTimeout: time.Second}
}

func newRebootRunner(x *fakeExec) *guestRunner {
	return &guestRunner{exec: x, vmid: "101", logPath: "", log: nil}
}

// bootTimeProbesAfterShutdown counts the kern.boottime reads issued after
// shutdown -r +0.
func bootTimeProbesAfterShutdown(x *fakeExec) int {
	argvs := x.argvs()
	shutdownAt := slices.Index(argvs, "shutdown -r +0")
	if shutdownAt < 0 {
		return 0
	}
	count := 0
	for _, argv := range argvs[shutdownAt+1:] {
		if argv == guestSysctl+" -n "+sysctlBootTime {
			count++
		}
	}
	return count
}

func TestRebootGuestWaitsForBootTimeToAdvance(t *testing.T) {
	t.Parallel()
	_, _, _, x, _ := newDeps(t)
	x.firmware.stagedRelease = "27.1"
	x.firmware.oldBootCommands = 3
	x.firmware.unreachableCommands = 2
	clk := &steppingClock{now: time.Unix(1_700_000_000, 0), step: time.Second}
	runner := newRebootRunner(x)

	if err := rebootGuest(context.Background(), clk, runner, testRebootWait()); err != nil {
		t.Fatalf("rebootGuest: %v", err)
	}
	if x.firmware.bootSeconds != testbedBootSeconds+bootSecondsPerReboot {
		t.Fatalf("boot seconds = %d, want the new boot", x.firmware.bootSeconds)
	}
	if probes := bootTimeProbesAfterShutdown(x); probes != 6 {
		t.Fatalf("boot time probes after shutdown = %d, want 6 (3 old boot, 2 unreachable, 1 new boot)", probes)
	}
	post, err := captureFirmwareState(context.Background(), runner)
	if err != nil {
		t.Fatalf("captureFirmwareState after reboot: %v", err)
	}
	if post.CoreVersion != "27.1" {
		t.Fatalf("core version after reboot = %q, want the staged 27.1, not the old boot's state", post.CoreVersion)
	}
}

func TestRebootGuestTimesOutWhenBootTimeNeverAdvances(t *testing.T) {
	t.Parallel()
	_, _, _, x, _ := newDeps(t)
	x.firmware.rebootIgnored = true
	clk := &steppingClock{now: time.Unix(1_700_000_000, 0), step: time.Minute}

	err := rebootGuest(context.Background(), clk, newRebootRunner(x), testRebootWait())
	if err == nil {
		t.Fatalf("rebootGuest succeeded although the guest never left boot %d", x.firmware.bootSeconds)
	}
	if !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("error %q does not report the unchanged boot time", err)
	}
	if x.firmware.reboots != 1 {
		t.Fatalf("reboots = %d, want 1", x.firmware.reboots)
	}
	if probes := bootTimeProbesAfterShutdown(x); probes < 2 {
		t.Fatalf("boot time probes after shutdown = %d, want several before the timeout", probes)
	}
}

func TestExecuteDoesNotRebootWhenBootTimeCaptureFails(t *testing.T) {
	t.Parallel()
	deps, _, _, x, _ := newDeps(t)
	// Each clock read passes a whole reboot timeout, so a reboot issued by
	// mistake ends in one probe instead of waiting on a frozen clock.
	deps.Clock = &steppingClock{now: time.Unix(1_700_000_000, 0), step: DefaultPostRebootTimeout}
	x.firmware.coreAvailable = "26.7.4"
	x.firmware.updaterAvailable = "26.7.4"
	x.byArgv[guestSysctl+" -n "+sysctlBootTime] = GuestExecResult{
		ExitCode: 1, Stdout: "", Stderr: "sysctl: unknown oid 'kern.boottime'\n",
	}
	opts := newOpts(t, "101")

	st, _, err := prepareAndExecute(t, deps, opts)
	if err == nil {
		t.Fatalf("Execute succeeded although the boot time could not be read")
	}
	if st.Phase != PhaseExecuteFailed {
		t.Fatalf("phase = %q, want execute_failed", st.Phase)
	}
	if !strings.Contains(err.Error(), "read boot time before reboot") {
		t.Fatalf("error %q does not name the boot time read", err)
	}
	if x.firmware.reboots != 0 || slices.Contains(x.argvs(), "shutdown -r +0") {
		t.Fatalf("rebooted without a boot time to compare: %v", x.argvs())
	}
}

func TestParseBootTimeReadsTestbedOutput(t *testing.T) {
	t.Parallel()
	got, err := parseBootTime(context.Background(), "{ sec = 1786126956, usec = 209257 } Fri Aug  7 11:22:36 2026\n")
	if err != nil {
		t.Fatalf("parseBootTime: %v", err)
	}
	want := time.Unix(testbedBootSeconds, testbedBootMicroseconds*int64(time.Microsecond)).UTC()
	if !got.Equal(want) {
		t.Fatalf("boot time = %s, want %s", got, want)
	}
	if _, err := parseBootTime(context.Background(), "sysctl: unknown oid 'kern.boottime'"); err == nil {
		t.Fatalf("parseBootTime accepted output without a boot time")
	}
}
