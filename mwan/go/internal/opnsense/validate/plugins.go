package validate

import (
	"context"
	"fmt"
	"strings"
)

// pluginCheckIDFor returns the canonical id for an os-* plugin
// installed/running pair. The helper keeps the snake_case format
// consistent across the matrix.
func pluginCheckIDFor(name, kind string) string {
	return fmt.Sprintf("plugin_%s_%s", strings.ReplaceAll(name, "-", "_"), kind)
}

// pluginInstalledCheck is `pkg info <plugin>` exit code.
type pluginInstalledCheck struct {
	plugin string
	sev    Severity
}

// NewPluginInstalledCheck returns the install check for a plugin.
// os-frr is severity blocker, all other plugins are regression.
func NewPluginInstalledCheck(plugin string) Check {
	sev := SeverityRegression
	if plugin == "os-frr" {
		sev = SeverityBlocker
	}
	return &pluginInstalledCheck{plugin: plugin, sev: sev}
}

func (c *pluginInstalledCheck) ID() string {
	return pluginCheckIDFor(c.plugin, "installed")
}
func (c *pluginInstalledCheck) Category() Category { return CategoryPlugins }
func (c *pluginInstalledCheck) Severity() Severity { return c.sev }

func (c *pluginInstalledCheck) AppliesWhen(b *Baseline) bool {
	if c.plugin == "os-frr" {
		return true
	}
	if b == nil {
		return true
	}
	return b.HasPlugin(c.plugin)
}

func (c *pluginInstalledCheck) Run(ctx context.Context, env Env) Result {
	command := "pkg info " + c.plugin
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode == 0 {
		res.Outcome = OutcomePass
		res.ParsedValue = "installed"
		return res
	}
	res.Outcome = OutcomeFail
	res.ParsedValue = "missing"
	res.Message = fmt.Sprintf("pkg info exit %d", cmd.ExitCode)
	return res
}

// pluginRunningCheck is `service <name> status` exit code.
type pluginRunningCheck struct {
	plugin      string
	serviceName string
	sev         Severity
}

// NewPluginRunningCheck returns the service-status check.
func NewPluginRunningCheck(plugin, serviceName string) Check {
	sev := SeverityRegression
	if plugin == "os-frr" {
		sev = SeverityBlocker
	}
	return &pluginRunningCheck{plugin: plugin, serviceName: serviceName, sev: sev}
}

func (c *pluginRunningCheck) ID() string         { return pluginCheckIDFor(c.plugin, "running") }
func (c *pluginRunningCheck) Category() Category { return CategoryPlugins }
func (c *pluginRunningCheck) Severity() Severity { return c.sev }

func (c *pluginRunningCheck) AppliesWhen(b *Baseline) bool {
	if c.plugin == "os-frr" {
		return true
	}
	if b == nil {
		return true
	}
	return b.HasPlugin(c.plugin)
}

func (c *pluginRunningCheck) Run(ctx context.Context, env Env) Result {
	command := fmt.Sprintf("service %s status", c.serviceName)
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode == 0 || strings.Contains(cmd.Stdout, "is running") {
		res.Outcome = OutcomePass
		res.ParsedValue = "running"
		return res
	}
	res.Outcome = OutcomeFail
	res.ParsedValue = "not_running"
	res.Message = fmt.Sprintf("service exit %d", cmd.ExitCode)
	return res
}

// pluginVersionCheck records the installed plugin version. A
// version drift is reported as advisory by the diff layer.
type pluginVersionCheck struct {
	plugin string
}

// NewPluginVersionCheck returns the version-record check.
func NewPluginVersionCheck(plugin string) Check {
	return &pluginVersionCheck{plugin: plugin}
}

func (c *pluginVersionCheck) ID() string         { return pluginCheckIDFor(c.plugin, "version") }
func (c *pluginVersionCheck) Category() Category { return CategoryPlugins }
func (c *pluginVersionCheck) Severity() Severity { return SeverityAdvisory }

func (c *pluginVersionCheck) AppliesWhen(b *Baseline) bool {
	if b == nil {
		return true
	}
	return b.HasPlugin(c.plugin)
}

func (c *pluginVersionCheck) Run(ctx context.Context, env Env) Result {
	command := fmt.Sprintf(`pkg info %s | awk '/^Version/{print $3}'`, c.plugin)
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	value := strings.TrimSpace(cmd.Stdout)
	res.ParsedValue = value
	res.Outcome = OutcomePass
	return res
}

// pkgAuditCheck runs `pkg audit -F` then `pkg audit` and reports
// vulnerable packages.
type pkgAuditCheck struct{}

// NewPkgAuditCheck returns the package-audit check.
func NewPkgAuditCheck() Check { return &pkgAuditCheck{} }

func (c *pkgAuditCheck) ID() string                   { return "pkg_audit_no_vulnerable_packages" }
func (c *pkgAuditCheck) Category() Category           { return CategoryPlugins }
func (c *pkgAuditCheck) Severity() Severity           { return SeverityAdvisory }
func (c *pkgAuditCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *pkgAuditCheck) Run(ctx context.Context, env Env) Result {
	command := `pkg audit -F >/dev/null 2>&1; pkg audit; echo $?`
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	lines := strings.Split(strings.TrimSpace(cmd.Stdout), "\n")
	last := ""
	if len(lines) > 0 {
		last = strings.TrimSpace(lines[len(lines)-1])
	}
	res.ParsedValue = last
	if last == "0" {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = "pkg audit reported vulnerable packages"
	return res
}
