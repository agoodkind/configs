package validate

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const (
	// CheckIDDNSResolvesExternal is the matrix id for the external dig probe.
	CheckIDDNSResolvesExternal = "dns_resolves_external"
	// CheckIDDNSResolvesInternal is the matrix id for the internal dig probe.
	CheckIDDNSResolvesInternal = "dns_resolves_internal"
	// CheckIDUnboundRunning is the matrix id for the unbound presence check.
	CheckIDUnboundRunning = "unbound_running"
	// CheckIDDHCPv4LeasesPresent is the matrix id for the v4 lease check.
	CheckIDDHCPv4LeasesPresent = "dhcpv4_leases_present"
	// CheckIDDHCPv6IANAPresent is the matrix id for the IA-NA lease check.
	CheckIDDHCPv6IANAPresent = "dhcpv6_ia_na_present"
	// CheckIDDHCPv6IAPDPresent is the matrix id for the IA-PD lease check.
	CheckIDDHCPv6IAPDPresent = "dhcpv6_ia_pd_present"
	// CheckIDRadvdAnnouncing is the matrix id for the radvd presence check.
	CheckIDRadvdAnnouncing = "radvd_announcing"
)

// dnsResolveCheck wraps both external and internal dig probes.
type dnsResolveCheck struct {
	id     string
	target string
	sev    Severity
}

// NewDNSResolveExternalCheck returns the external dig probe.
func NewDNSResolveExternalCheck(opnsenseLAN string, externalName string) Check {
	if externalName == "" {
		externalName = "ifconfig.co"
	}
	return &dnsResolveCheck{
		id:     CheckIDDNSResolvesExternal,
		target: fmt.Sprintf("dig +short +time=3 +tries=1 @%s %s", opnsenseLAN, externalName),
		sev:    SeverityBlocker,
	}
}

// NewDNSResolveInternalCheck returns the internal dig probe.
func NewDNSResolveInternalCheck(opnsenseLAN string, internalName string) Check {
	if internalName == "" {
		internalName = "router.home.goodkind.io"
	}
	return &dnsResolveCheck{
		id:     CheckIDDNSResolvesInternal,
		target: fmt.Sprintf("dig +short @%s %s", opnsenseLAN, internalName),
		sev:    SeverityRegression,
	}
}

func (c *dnsResolveCheck) ID() string                   { return c.id }
func (c *dnsResolveCheck) Category() Category           { return CategoryDNSDHCP }
func (c *dnsResolveCheck) Severity() Severity           { return c.sev }
func (c *dnsResolveCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *dnsResolveCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	out, err := env.LANClientExec(ctx, c.target)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	res.RawStdout = out.Stdout
	res.RawExitCode = out.ExitCode
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	answer := strings.TrimSpace(out.Stdout)
	res.ParsedValue = answer
	if out.ExitCode == 0 && answer != "" {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("dig exit=%d answer=%q", out.ExitCode, answer)
	return res
}

// unboundRunningCheck verifies the unbound process is up.
type unboundRunningCheck struct{}

// NewUnboundRunningCheck returns the unbound presence check.
func NewUnboundRunningCheck() Check { return &unboundRunningCheck{} }

func (c *unboundRunningCheck) ID() string                   { return CheckIDUnboundRunning }
func (c *unboundRunningCheck) Category() Category           { return CategoryDNSDHCP }
func (c *unboundRunningCheck) Severity() Severity           { return SeverityBlocker }
func (c *unboundRunningCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *unboundRunningCheck) Run(ctx context.Context, env Env) Result {
	command := "pgrep -f unbound | wc -l"
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	count := parseIntOrZero(strings.TrimSpace(cmd.Stdout))
	res.ParsedValue = strconv.Itoa(count)
	if count >= 1 {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = "unbound process not found"
	return res
}

// dhcpLeaseCountCheck counts active DHCPv4 or DHCPv6 leases.
type dhcpLeaseCountCheck struct {
	id     string
	cmd    string
	sev    Severity
	skipIf func(*Baseline) bool
}

// NewDHCPv4LeaseCountCheck returns the v4 lease check.
func NewDHCPv4LeaseCountCheck() Check {
	return &dhcpLeaseCountCheck{
		id:     CheckIDDHCPv4LeasesPresent,
		cmd:    `cat /var/dhcpd/var/db/dhcpd.leases 2>/dev/null | grep -c '^lease '`,
		sev:    SeverityRegression,
		skipIf: nil,
	}
}

// NewDHCPv6IANALeaseCountCheck returns the IA-NA lease check.
func NewDHCPv6IANALeaseCountCheck() Check {
	return &dhcpLeaseCountCheck{
		id:  CheckIDDHCPv6IANAPresent,
		cmd: `cat /var/dhcpd/var/db/dhcpd6.leases 2>/dev/null | grep -c 'ia-na '`,
		sev: SeverityRegression,
		skipIf: func(b *Baseline) bool {
			return b != nil && b.DHCPv6IANALeaseCount == 0
		},
	}
}

// NewDHCPv6IAPDLeaseCountCheck returns the IA-PD lease check.
func NewDHCPv6IAPDLeaseCountCheck() Check {
	return &dhcpLeaseCountCheck{
		id:  CheckIDDHCPv6IAPDPresent,
		cmd: `cat /var/dhcpd/var/db/dhcpd6.leases 2>/dev/null | grep -c 'ia-pd '`,
		sev: SeverityRegression,
		skipIf: func(b *Baseline) bool {
			return b != nil && b.DHCPv6IAPDLeaseCount == 0
		},
	}
}

func (c *dhcpLeaseCountCheck) ID() string         { return c.id }
func (c *dhcpLeaseCountCheck) Category() Category { return CategoryDNSDHCP }
func (c *dhcpLeaseCountCheck) Severity() Severity { return c.sev }

func (c *dhcpLeaseCountCheck) AppliesWhen(b *Baseline) bool {
	if c.skipIf == nil {
		return true
	}
	return !c.skipIf(b)
}

func (c *dhcpLeaseCountCheck) Run(ctx context.Context, env Env) Result {
	res, cmd, ok := runOPNsenseCommand(ctx, env, c.cmd, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	count := parseIntOrZero(strings.TrimSpace(cmd.Stdout))
	res.ParsedValue = strconv.Itoa(count)
	res.Outcome = OutcomePass
	return res
}

// radvdAnnouncingCheck verifies radvd is running with at least one
// configured announcement.
type radvdAnnouncingCheck struct{}

// NewRadvdAnnouncingCheck returns the radvd check.
func NewRadvdAnnouncingCheck() Check { return &radvdAnnouncingCheck{} }

func (c *radvdAnnouncingCheck) ID() string                   { return CheckIDRadvdAnnouncing }
func (c *radvdAnnouncingCheck) Category() Category           { return CategoryDNSDHCP }
func (c *radvdAnnouncingCheck) Severity() Severity           { return SeverityRegression }
func (c *radvdAnnouncingCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *radvdAnnouncingCheck) Run(ctx context.Context, env Env) Result {
	command := `pgrep -af radvd | grep -c radvd.conf`
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	count := parseIntOrZero(strings.TrimSpace(cmd.Stdout))
	res.ParsedValue = strconv.Itoa(count)
	if count >= 1 {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = "radvd not running with configured announcements"
	return res
}
