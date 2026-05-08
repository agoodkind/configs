// Package validate implements the MWAN-153 OPNsense 26.x upgrade test
// matrix runner. The matrix lives in
// mwan/docs/MWAN-153-26x-upgrade-test-matrix.md and this package turns
// each row into a typed Check that produces a deterministic Result.
//
// The runner is invoked as a subcommand of the mwan monolith
// (mwan opnsense-validate) and is also callable from the MWAN-152
// upgrade flow via Run.
package validate

import (
	"context"
	"time"
)

// Severity classifies the impact of a failed check. The matrix
// document defines three tiers; the runner aggregates them when it
// produces an overall verdict.
type Severity string

const (
	// SeverityBlocker means a failed check fails the upgrade and
	// triggers the MWAN-152 rollback path.
	SeverityBlocker Severity = "blocker"

	// SeverityRegression means a feature broke but the upgrade
	// itself stands; the operator must triage before declaring
	// success.
	SeverityRegression Severity = "regression"

	// SeverityAdvisory means a value drifted in an expected way
	// (plugin version bump, kernel default flip).
	SeverityAdvisory Severity = "advisory"
)

// Outcome is the per-check verdict.
type Outcome string

const (
	// OutcomePass means the check ran and matched its expectation.
	OutcomePass Outcome = "pass"

	// OutcomeFail means the check ran but did not match.
	OutcomeFail Outcome = "fail"

	// OutcomeSkip means the check did not run because its
	// applies-when predicate evaluated false against the baseline.
	OutcomeSkip Outcome = "skip"

	// OutcomeError means the check could not run because the
	// underlying transport returned an unrecoverable error.
	OutcomeError Outcome = "error"
)

// Category groups checks by surface so the operator can filter
// reports per surface. The strings match the headings in the matrix.
type Category string

// CategoryRouting is the routing surface category. The remaining
// constants in this block are the matrix surface category values.
const (
	CategoryRouting Category = "routing"
	// CategoryDNSDHCP is the DNS/DHCP surface category.
	CategoryDNSDHCP Category = "dns_dhcp"
	// CategoryFirewall is the firewall (pf) surface category.
	CategoryFirewall Category = "firewall"
	// CategoryPlugins is the plugins surface category.
	CategoryPlugins Category = "plugins"
	// CategoryMWAN is the MWAN integration surface category.
	CategoryMWAN Category = "mwan"
	// CategoryWebAPI is the OPNsense Web/API surface category.
	CategoryWebAPI Category = "web_api"
	// CategoryMonitoring is the off-box monitoring surface category.
	CategoryMonitoring Category = "monitoring"
	// CategoryKernel is the kernel/driver surface category.
	CategoryKernel Category = "kernel"
)

// Result is a single check's output. The shape is stable and
// serialised to baseline / post / diff JSON files.
type Result struct {
	CheckID     string    `json:"check_id"`
	Category    Category  `json:"category"`
	Severity    Severity  `json:"severity"`
	Outcome     Outcome   `json:"outcome"`
	Message     string    `json:"message,omitempty"`
	ParsedValue string    `json:"parsed_value,omitempty"`
	RawStdout   string    `json:"raw_stdout,omitempty"`
	RawExitCode int       `json:"raw_exit_code"`
	DurationMs  int64     `json:"duration_ms"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
}

// Check is the per-row interface. Each matrix row implements one of
// these. The runner enumerates registered checks, evaluates their
// applies-when predicate against the baseline, then calls Run.
type Check interface {
	// ID returns the snake_case identifier from the matrix.
	ID() string

	// Category returns the surface tag.
	Category() Category

	// Severity returns the severity tier.
	Severity() Severity

	// AppliesWhen reports whether this check should run given the
	// captured baseline. A check that returns false here is reported
	// as OutcomeSkip with a short reason. The baseline is nil during
	// a baseline-capture run, in which case the check should run.
	AppliesWhen(baseline *Baseline) bool

	// Run executes the check against the live environment and
	// returns its parsed result.
	Run(ctx context.Context, env Env) Result
}

// Env is the abstract execution environment exposed to checks. It
// hides the concrete transport (SSH, gRPC, local exec, HTTP) so unit
// tests can drive every check with a fake.
type Env interface {
	// SSHOPNsense runs a shell command on the OPNsense guest. The
	// implementation is expected to wrap an SSH transport with the
	// same auth path as the cutover flow. Returns stdout, stderr,
	// exit code, and any transport error.
	SSHOPNsense(ctx context.Context, command string) (CommandResult, error)

	// SSHProxmoxHost runs a shell command on the Proxmox host
	// (vault). Used for QGA, mwan opnsense-host socket, and
	// watchdog log queries.
	SSHProxmoxHost(ctx context.Context, command string) (CommandResult, error)

	// LANClientExec runs a shell command from a LAN client. The
	// concrete implementation is operator-supplied (for example, a
	// helper VM SSHed into via the Proxmox host). Used for the
	// data-plane probes (curl, dig, nc).
	LANClientExec(ctx context.Context, command string) (CommandResult, error)

	// OPNsenseHTTPSGet fetches a URL on the OPNsense web UI.
	// Implementations skip TLS verification because the UI cert is
	// self-signed in our deployment.
	OPNsenseHTTPSGet(ctx context.Context, path string, basicAuth *BasicAuth) (HTTPResult, error)

	// Now returns the current time. Indirected so tests can pin
	// timestamps deterministically.
	Now() time.Time
}

// CommandResult bundles the shell command output for a check.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// newResult constructs a Result with every field initialised so the
// exhaustruct linter has no missing-field complaints. Callers fill
// in only the fields they care about; the rest stay at their zero
// values.
func newResult(id string, cat Category, sev Severity, started, finished time.Time) Result {
	return Result{
		CheckID:     id,
		Category:    cat,
		Severity:    sev,
		Outcome:     "",
		Message:     "",
		ParsedValue: "",
		RawStdout:   "",
		RawExitCode: 0,
		DurationMs:  finished.Sub(started).Milliseconds(),
		StartedAt:   started,
		FinishedAt:  finished,
	}
}

// emptyResult returns a fully-zeroed Result. The exhaustruct
// linter rejects bare `Result{}` literals; this helper supplies
// every field at its zero value so callers can use a placeholder
// without each tripping the rule.
func emptyResult() Result {
	return Result{
		CheckID:     "",
		Category:    "",
		Severity:    "",
		Outcome:     "",
		Message:     "",
		ParsedValue: "",
		RawStdout:   "",
		RawExitCode: 0,
		DurationMs:  0,
		StartedAt:   time.Time{},
		FinishedAt:  time.Time{},
	}
}

// HTTPResult bundles the HTTP response surface relevant to checks.
type HTTPResult struct {
	StatusCode int
	Body       string
}

// BasicAuth is the credential pair for OPNsense API requests.
type BasicAuth struct {
	Username string
	Password string
}
