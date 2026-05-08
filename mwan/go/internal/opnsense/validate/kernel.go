package validate

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	// CheckIDVtnetHWLRODisabled is the matrix id for the vtnet HW LRO check.
	CheckIDVtnetHWLRODisabled = "vtnet_hwlro_disabled"
	// CheckIDInterfacesSetUnchanged is the matrix id for the interface-set check.
	CheckIDInterfacesSetUnchanged = "interfaces_set_unchanged"
)

// vtnetHWLRODisabledCheck reads dev.vtnet.<i>.tx_hwlro for each
// vtnet index recorded in baseline. Resolved decision O-5 (R5)
// expects 0 on 26.x.
type vtnetHWLRODisabledCheck struct{}

// NewVtnetHWLRODisabledCheck returns the vtnet HW LRO check.
func NewVtnetHWLRODisabledCheck() Check { return &vtnetHWLRODisabledCheck{} }

func (c *vtnetHWLRODisabledCheck) ID() string         { return CheckIDVtnetHWLRODisabled }
func (c *vtnetHWLRODisabledCheck) Category() Category { return CategoryKernel }
func (c *vtnetHWLRODisabledCheck) Severity() Severity { return SeverityAdvisory }

func (c *vtnetHWLRODisabledCheck) AppliesWhen(b *Baseline) bool {
	if b == nil {
		return true
	}
	return len(b.VtnetIndices) > 0
}

func (c *vtnetHWLRODisabledCheck) Run(ctx context.Context, env Env) Result {
	command := `for i in $(sysctl -aN dev.vtnet 2>/dev/null | grep tx_hwlro); do echo "$i=$(sysctl -n $i)"; done`
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode != 0 {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("sysctl exit %d", cmd.ExitCode)
		return res
	}
	out := strings.TrimSpace(cmd.Stdout)
	res.ParsedValue = out
	allZero := true
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasSuffix(line, "=0") {
			allZero = false
			break
		}
	}
	if allZero {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = "at least one vtnet has tx_hwlro != 0"
	return res
}

// interfacesSetUnchangedCheck compares `ifconfig -l` to baseline.
type interfacesSetUnchangedCheck struct{}

// NewInterfacesSetUnchangedCheck returns the interface-set check.
func NewInterfacesSetUnchangedCheck() Check { return &interfacesSetUnchangedCheck{} }

func (c *interfacesSetUnchangedCheck) ID() string         { return CheckIDInterfacesSetUnchanged }
func (c *interfacesSetUnchangedCheck) Category() Category { return CategoryKernel }
func (c *interfacesSetUnchangedCheck) Severity() Severity { return SeverityRegression }

func (c *interfacesSetUnchangedCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *interfacesSetUnchangedCheck) Run(ctx context.Context, env Env) Result {
	command := `ifconfig -l | tr ' ' '\n' | sort`
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode != 0 {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("ifconfig exit %d", cmd.ExitCode)
		return res
	}
	ifaces := []string{}
	for line := range strings.SplitSeq(cmd.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ifaces = append(ifaces, line)
		}
	}
	sort.Strings(ifaces)
	res.ParsedValue = strings.Join(ifaces, ",")
	res.Outcome = OutcomePass
	return res
}
