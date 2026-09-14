package upgrade

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The reboot wait proves the guest came back on a new boot by reading the
// kernel's boot time. FreeBSD shutdown forks and its parent exits 0 before
// rc.shutdown runs (sbin/shutdown/shutdown.c:249-253), so a guest that
// answers right after shutdown -r can still be the boot that is going down.
const (
	guestSysctl    = "/sbin/sysctl"
	sysctlBootTime = "kern.boottime"
	// rebootPollInterval is the pause between boot time probes.
	rebootPollInterval = 2 * time.Second
	// rebootProbeTimeout bounds one probe, so a guest that stops answering
	// mid-call cannot hold the wait past its deadline.
	rebootProbeTimeout = 30 * time.Second
)

const microsecondsPerSecond = 1_000_000

// bootTimePattern matches sysctl -n kern.boottime output, for example
// "{ sec = 1786126956, usec = 209257 } Fri Aug  7 11:22:36 2026".
var bootTimePattern = regexp.MustCompile(`^\{ sec = (\d+), usec = (\d+) \}`)

// rebootWait bounds the wait for a new boot: the whole wait, the pause
// between probes, and each probe.
type rebootWait struct {
	timeout      time.Duration
	interval     time.Duration
	probeTimeout time.Duration
}

func defaultRebootWait() rebootWait {
	return rebootWait{
		timeout:      DefaultPostRebootTimeout,
		interval:     rebootPollInterval,
		probeTimeout: rebootProbeTimeout,
	}
}

// readBootTime reads the guest's boot time through the logged runner. The
// reboot step reads it before shutdown, so a failure there stops the
// reboot instead of leaving nothing to compare against.
func readBootTime(ctx context.Context, r *guestRunner) (time.Time, error) {
	out, err := r.value(ctx, guestSysctl, "-n", sysctlBootTime)
	if err != nil {
		return time.Time{}, err
	}
	return parseBootTime(ctx, out)
}

// parseBootTime converts sysctl -n kern.boottime output to a time.
func parseBootTime(ctx context.Context, out string) (time.Time, error) {
	match := bootTimePattern.FindStringSubmatch(strings.TrimSpace(out))
	if match == nil {
		err := fmt.Errorf("%s: unrecognised output %q", sysctlBootTime, out)
		slog.WarnContext(ctx, "upgrade: boot time output not recognised", "err", err)
		return time.Time{}, err
	}
	seconds, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		wrapped := fmt.Errorf("%s: seconds %q: %w", sysctlBootTime, match[1], err)
		slog.WarnContext(ctx, "upgrade: boot time seconds not parsed", "err", wrapped)
		return time.Time{}, wrapped
	}
	microseconds, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		wrapped := fmt.Errorf("%s: microseconds %q: %w", sysctlBootTime, match[2], err)
		slog.WarnContext(ctx, "upgrade: boot time microseconds not parsed", "err", wrapped)
		return time.Time{}, wrapped
	}
	if microseconds >= microsecondsPerSecond {
		rangeErr := fmt.Errorf("%s: microseconds %d out of range", sysctlBootTime, microseconds)
		slog.WarnContext(ctx, "upgrade: boot time microseconds out of range", "err", rangeErr)
		return time.Time{}, rangeErr
	}
	return time.Unix(seconds, microseconds*int64(time.Microsecond)).UTC(), nil
}

// waitForReboot polls the guest's boot time until it is strictly later
// than before, and returns the new boot time. A failed probe or the old
// boot time means the guest has not finished rebooting yet. The deadline
// comes from clk; it returns an error once the deadline passes or ctx ends.
func waitForReboot(
	ctx context.Context,
	clk Clock,
	exec Executor,
	vmid string,
	before time.Time,
	wait rebootWait,
) (time.Time, error) {
	start := clk.Now()
	deadline := start.Add(wait.timeout)
	var bootTime time.Time
	attempt := 0
	for {
		attempt++
		probed, answered := probeBootTime(ctx, exec, vmid, attempt, wait.probeTimeout)
		if answered && probed.After(before) {
			bootTime = probed
			break
		}
		if answered {
			slog.DebugContext(ctx, "upgrade: guest still reports the boot before the reboot",
				"vmid", vmid, "attempt", attempt, "boot_time", probed)
		}
		if !clk.Now().Before(deadline) {
			timedErr := fmt.Errorf("boot time did not advance past %s within %s",
				before.Format(time.RFC3339Nano), wait.timeout)
			slog.WarnContext(ctx, "upgrade: guest did not reach a new boot",
				"err", timedErr, "vmid", vmid, "attempts", attempt)
			return time.Time{}, timedErr
		}
		select {
		case <-ctx.Done():
			cancelErr := fmt.Errorf("wait for new boot: %w", ctx.Err())
			slog.WarnContext(ctx, "upgrade: reboot wait cancelled",
				"err", cancelErr, "vmid", vmid, "attempts", attempt)
			return time.Time{}, cancelErr
		case <-time.After(wait.interval):
		}
	}
	slog.InfoContext(ctx, "upgrade: guest is back on a new boot",
		"vmid", vmid, "attempts", attempt, "boot_time_before", before,
		"boot_time_after", bootTime, "waited", clk.Now().Sub(start))
	return bootTime, nil
}

// probeBootTime reads the boot time once without writing upgrade.log, since
// the wait can probe hundreds of times while the guest is down. It reports
// false, after logging why at debug level, when the guest did not answer
// with a boot time.
func probeBootTime(
	ctx context.Context, exec Executor, vmid string, attempt int, probeTimeout time.Duration,
) (time.Time, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	res, err := exec.GuestExec(probeCtx, vmid, guestSysctl, "-n", sysctlBootTime)
	if err != nil {
		slog.DebugContext(ctx, "upgrade: boot time probe failed",
			"err", err, "vmid", vmid, "attempt", attempt)
		return time.Time{}, false
	}
	if res.ExitCode != 0 {
		slog.DebugContext(ctx, "upgrade: boot time probe exited non-zero",
			"vmid", vmid, "attempt", attempt, "exit", res.ExitCode,
			"stderr", strings.TrimSpace(res.Stderr))
		return time.Time{}, false
	}
	bootTime, err := parseBootTime(ctx, res.Stdout)
	if err != nil {
		return time.Time{}, false
	}
	return bootTime, true
}
