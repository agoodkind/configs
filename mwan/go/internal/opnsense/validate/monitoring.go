package validate

import (
	"context"
	"fmt"
	"strings"
)

const (
	// CheckIDWatchdogPathHealthy is the matrix id for the watchdog-state check.
	CheckIDWatchdogPathHealthy = "watchdog_path_healthy"
	// CheckIDNotifyEmailPathIntact is the matrix id for the notify-self-test check.
	CheckIDNotifyEmailPathIntact = "notify_email_path_intact"
)

// watchdogPathHealthyCheck reads the most recent state from the
// mwan-watchdog journal on the Proxmox host. Resolved decision
// O-4 promotes this to a blocker because the in-monolith watchdog
// is the de facto external monitor.
type watchdogPathHealthyCheck struct{}

// NewWatchdogPathHealthyCheck returns the watchdog-state check.
func NewWatchdogPathHealthyCheck() Check { return &watchdogPathHealthyCheck{} }

func (c *watchdogPathHealthyCheck) ID() string                   { return CheckIDWatchdogPathHealthy }
func (c *watchdogPathHealthyCheck) Category() Category           { return CategoryMonitoring }
func (c *watchdogPathHealthyCheck) Severity() Severity           { return SeverityBlocker }
func (c *watchdogPathHealthyCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *watchdogPathHealthyCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	command := `journalctl -u mwan-watchdog --since '5 minutes ago' --no-pager | grep -E 'state=(OK|degraded|fault)' | tail -1`
	out, err := env.SSHProxmoxHost(ctx, command)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	res.RawStdout = out.Stdout
	res.RawExitCode = out.ExitCode
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	value := strings.TrimSpace(out.Stdout)
	res.ParsedValue = value
	if strings.Contains(value, "state=OK") {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("watchdog last state line %q", value)
	return res
}

// notifyEmailPathIntactCheck calls `mwan notify --self-test
// --dry-run` on the Proxmox host. The fallback per resolved
// decision O-4 is `mwan agent ping`; the runner shells out to
// either path and treats exit 0 as healthy. The exact command
// is embedded so a future binary that lacks the self-test still
// produces a clear failure message.
type notifyEmailPathIntactCheck struct{}

// NewNotifyEmailPathIntactCheck returns the notify-self-test check.
func NewNotifyEmailPathIntactCheck() Check { return &notifyEmailPathIntactCheck{} }

func (c *notifyEmailPathIntactCheck) ID() string                   { return CheckIDNotifyEmailPathIntact }
func (c *notifyEmailPathIntactCheck) Category() Category           { return CategoryMonitoring }
func (c *notifyEmailPathIntactCheck) Severity() Severity           { return SeverityRegression }
func (c *notifyEmailPathIntactCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *notifyEmailPathIntactCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	command := `mwan notify --self-test --dry-run 2>/dev/null || mwan agent ping`
	out, err := env.SSHProxmoxHost(ctx, command)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	res.RawStdout = out.Stdout
	res.RawExitCode = out.ExitCode
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	if out.ExitCode == 0 {
		res.Outcome = OutcomePass
		res.ParsedValue = "notify_path_ok"
		return res
	}
	res.Outcome = OutcomeFail
	res.ParsedValue = "notify_path_fail"
	res.Message = fmt.Sprintf("notify self-test/agent ping exit %d", out.ExitCode)
	return res
}
