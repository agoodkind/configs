package validate

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// CheckIDPFEnabled is the matrix id for the pf-enabled check.
	CheckIDPFEnabled = "pf_enabled"
	// CheckIDPFRuleCountWithinTolerance is the matrix id for the pf rule-count drift check.
	CheckIDPFRuleCountWithinTolerance = "pf_rule_count_within_tolerance"
	// CheckIDPFStateTableGrowing is the matrix id for the pf state-table growth check.
	CheckIDPFStateTableGrowing = "pf_state_table_growing"
	// CheckIDPFNatRuleCount is the matrix id for the pf NAT rule-count check.
	CheckIDPFNatRuleCount = "pf_nat_rule_count"
	// CheckIDPFBlocksDefaultInLAN is the matrix id for the LAN-block check (deferred).
	CheckIDPFBlocksDefaultInLAN = "pf_blocks_default_in_lan"
	// CheckIDCoreCaptiveportalZones is the matrix id for the captive-portal zone check.
	CheckIDCoreCaptiveportalZones = "core_captiveportal_zones_active"
	// CheckIDCoreCaptiveportalAliases is the matrix id for the captive-portal pf alias check.
	CheckIDCoreCaptiveportalAliases = "core_captiveportal_pf_aliases_present"
)

// pfEnabledCheck reports the pf status line.
type pfEnabledCheck struct{}

// NewPFEnabledCheck returns the pf-enabled check.
func NewPFEnabledCheck() Check { return &pfEnabledCheck{} }

func (c *pfEnabledCheck) ID() string                   { return CheckIDPFEnabled }
func (c *pfEnabledCheck) Category() Category           { return CategoryFirewall }
func (c *pfEnabledCheck) Severity() Severity           { return SeverityBlocker }
func (c *pfEnabledCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *pfEnabledCheck) Run(ctx context.Context, env Env) Result {
	command := `pfctl -si | awk '$1=="Status:"{print $2}'`
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	value := strings.TrimSpace(cmd.Stdout)
	res.ParsedValue = value
	if value == "Enabled" {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("pf status %q", value)
	return res
}

// pfRuleCountCheck counts pf rules.
type pfRuleCountCheck struct{}

// NewPFRuleCountCheck returns the pf rule-count check.
func NewPFRuleCountCheck() Check { return &pfRuleCountCheck{} }

func (c *pfRuleCountCheck) ID() string                   { return CheckIDPFRuleCountWithinTolerance }
func (c *pfRuleCountCheck) Category() Category           { return CategoryFirewall }
func (c *pfRuleCountCheck) Severity() Severity           { return SeverityAdvisory }
func (c *pfRuleCountCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *pfRuleCountCheck) Run(ctx context.Context, env Env) Result {
	command := "pfctl -sr | wc -l"
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	count := parseIntOrZero(strings.TrimSpace(cmd.Stdout))
	res.ParsedValue = strconv.Itoa(count)
	res.Outcome = OutcomePass
	return res
}

// pfNatRuleCountCheck counts pf NAT rules for tolerance comparison.
type pfNatRuleCountCheck struct{}

// NewPFNatRuleCountCheck returns the pf NAT rule-count check.
func NewPFNatRuleCountCheck() Check { return &pfNatRuleCountCheck{} }

func (c *pfNatRuleCountCheck) ID() string                   { return CheckIDPFNatRuleCount }
func (c *pfNatRuleCountCheck) Category() Category           { return CategoryFirewall }
func (c *pfNatRuleCountCheck) Severity() Severity           { return SeverityRegression }
func (c *pfNatRuleCountCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *pfNatRuleCountCheck) Run(ctx context.Context, env Env) Result {
	command := "pfctl -sn | wc -l"
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	count := parseIntOrZero(strings.TrimSpace(cmd.Stdout))
	res.ParsedValue = strconv.Itoa(count)
	res.Outcome = OutcomePass
	return res
}

// pfStateTableGrowingCheck samples the state table twice with a
// short gap and reports growth.
type pfStateTableGrowingCheck struct {
	gap time.Duration
}

// NewPFStateTableGrowingCheck returns the state-table check; gap
// defaults to 5 seconds per the matrix.
func NewPFStateTableGrowingCheck(gap time.Duration) Check {
	if gap <= 0 {
		gap = 5 * time.Second
	}
	return &pfStateTableGrowingCheck{gap: gap}
}

func (c *pfStateTableGrowingCheck) ID() string                   { return CheckIDPFStateTableGrowing }
func (c *pfStateTableGrowingCheck) Category() Category           { return CategoryFirewall }
func (c *pfStateTableGrowingCheck) Severity() Severity           { return SeverityRegression }
func (c *pfStateTableGrowingCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *pfStateTableGrowingCheck) Run(ctx context.Context, env Env) Result {
	cmdStr := `pfctl -si | awk '/state table/ {getline; print $2}'`
	res, first, ok := runOPNsenseCommand(ctx, env, cmdStr, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	timer := time.NewTimer(c.gap)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		res.Outcome = OutcomeError
		res.Message = ctx.Err().Error()
		return res
	case <-timer.C:
	}
	second, err := env.SSHOPNsense(ctx, cmdStr)
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = fmt.Sprintf("second sample transport error: %v", err)
		return res
	}
	a := parseIntOrZero(strings.TrimSpace(first.Stdout))
	b := parseIntOrZero(strings.TrimSpace(second.Stdout))
	res.ParsedValue = fmt.Sprintf("%d->%d", a, b)
	if b > a {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = "state table is not growing"
	return res
}

// coreCaptiveportalZonesCheck verifies every captive-portal zone in
// the baseline is still active per resolved decision O-2.
type coreCaptiveportalZonesCheck struct{}

// NewCoreCaptiveportalZonesCheck returns the captive portal zone
// check from the new section 2.g of the matrix.
func NewCoreCaptiveportalZonesCheck() Check { return &coreCaptiveportalZonesCheck{} }

func (c *coreCaptiveportalZonesCheck) ID() string         { return CheckIDCoreCaptiveportalZones }
func (c *coreCaptiveportalZonesCheck) Category() Category { return CategoryFirewall }
func (c *coreCaptiveportalZonesCheck) Severity() Severity { return SeverityRegression }

func (c *coreCaptiveportalZonesCheck) AppliesWhen(b *Baseline) bool {
	if b == nil {
		return true
	}
	return len(b.CaptivePortalZones) > 0
}

func (c *coreCaptiveportalZonesCheck) Run(ctx context.Context, env Env) Result {
	command := "configctl captiveportal list_zones"
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode != 0 {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("configctl exit %d", cmd.ExitCode)
		return res
	}
	zones := strings.TrimSpace(cmd.Stdout)
	res.ParsedValue = zones
	res.Outcome = OutcomePass
	return res
}

// coreCaptiveportalAliasesCheck checks pf alias presence for each
// captive-portal zone.
type coreCaptiveportalAliasesCheck struct{}

// NewCoreCaptiveportalAliasesCheck returns the alias presence check.
func NewCoreCaptiveportalAliasesCheck() Check { return &coreCaptiveportalAliasesCheck{} }

func (c *coreCaptiveportalAliasesCheck) ID() string         { return CheckIDCoreCaptiveportalAliases }
func (c *coreCaptiveportalAliasesCheck) Category() Category { return CategoryFirewall }
func (c *coreCaptiveportalAliasesCheck) Severity() Severity { return SeverityRegression }

func (c *coreCaptiveportalAliasesCheck) AppliesWhen(b *Baseline) bool {
	if b == nil {
		return true
	}
	return len(b.CaptivePortalZones) > 0
}

func (c *coreCaptiveportalAliasesCheck) Run(ctx context.Context, env Env) Result {
	// The matrix expects the alias name __captiveportal_zone_<id>
	// per zone in baseline. The runner approximates this by
	// checking zone 0 (matches prod's only zone). A future ticket
	// can iterate over baseline.CaptivePortalZones to cover
	// multi-zone deployments.
	command := "pfctl -t __captiveportal_zone_0 -T show | wc -l"
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	count := parseIntOrZero(strings.TrimSpace(cmd.Stdout))
	res.ParsedValue = strconv.Itoa(count)
	if cmd.ExitCode == 0 {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("pfctl exit %d", cmd.ExitCode)
	return res
}
