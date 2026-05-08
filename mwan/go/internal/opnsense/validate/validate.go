package validate

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// Config bundles the runner's tunable inputs. Values that are
// genuinely operator-specific live here; per-check defaults are
// inside each Check constructor.
type Config struct {
	// VMID is the OPNsense VM identifier on the Proxmox host.
	VMID int

	// DeployID identifies the upgrade attempt and is used to
	// build the artefact directory path.
	DeployID string

	// StateDir is the artefact root; defaults to DefaultStateDir.
	StateDir string

	// BGPv4Neighbors and BGPv6Neighbors are operator-supplied
	// lists of expected BGP peers. Empty lists make the matching
	// checks skip.
	BGPv4Neighbors []string
	BGPv6Neighbors []string

	// OPNsenseLAN is the LAN-side address of OPNsense, used for
	// dig probes from a LAN client.
	OPNsenseLAN string

	// MWANOpnsenseSocket is the unix socket path on the Proxmox
	// host the gRPC probe targets.
	MWANOpnsenseSocket string

	// MWANOpnsenseHostSocket is the unix socket path the bridge
	// listens on.
	MWANOpnsenseHostSocket string

	// APIAuth carries the OPNsense API key/secret pair.
	APIAuth *BasicAuth

	// SettleAfterUpgrade is the dwell time before DHCP-related
	// checks run, per resolved decision O-1 (default 5 minutes).
	// Zero disables the dwell.
	SettleAfterUpgrade time.Duration

	// SeverityFilter optionally restricts which severities are
	// run. Empty means run every check. Used by the CLI's
	// `--severity-filter blocker` mode.
	SeverityFilter Severity
}

// newBaseline returns a fully zeroed Baseline so the exhaustruct
// linter has no missing-field complaints. Callers fill in the
// fields they own and leave the rest at zero values.
func newBaseline(vmid int, deployID string, capturedAt time.Time, results []Result) *Baseline {
	return &Baseline{
		SchemaVersion:        BaselineSchemaVersion,
		CapturedAt:           capturedAt,
		VMID:                 vmid,
		DeployID:             deployID,
		OPNsenseVersion:      "",
		ProxmoxNode:          "",
		Plugins:              nil,
		CaptivePortalZones:   nil,
		VtnetIndices:         nil,
		InterfacesSet:        nil,
		DHCPv4LeaseCount:     0,
		DHCPv6IANALeaseCount: 0,
		DHCPv6IAPDLeaseCount: 0,
		PFRuleCount:          0,
		PFNatRuleCount:       0,
		Results:              results,
	}
}

// Run executes every registered check, applies AppliesWhen against
// the supplied baseline (or nil for a baseline-capture run), and
// returns a fully populated Baseline record. The caller decides
// whether to persist it via SaveBaseline.
func Run(ctx context.Context, cfg Config, baseline *Baseline, env Env) (*Baseline, error) {
	if env == nil {
		return nil, fmt.Errorf("validate.Run: env required")
	}
	checks := DefaultChecks(cfg)
	if cfg.SeverityFilter != "" {
		checks = filterBySeverity(checks, cfg.SeverityFilter)
	}
	results := make([]Result, 0, len(checks))
	for _, check := range checks {
		select {
		case <-ctx.Done():
			slog.ErrorContext(ctx, "validate.Run cancelled",
				"err", ctx.Err().Error())
			return nil, fmt.Errorf("validate.Run cancelled: %w", ctx.Err())
		default:
		}
		if !check.AppliesWhen(baseline) {
			now := env.Now()
			skip := newResult(check.ID(), check.Category(), check.Severity(), now, now)
			skip.Outcome = OutcomeSkip
			skip.Message = "applies-when predicate evaluated false"
			results = append(results, skip)
			continue
		}
		results = append(results, check.Run(ctx, env))
	}
	out := newBaseline(cfg.VMID, cfg.DeployID, env.Now(), results)
	if baseline != nil {
		// Carry forward operator-specific shape so subsequent
		// runs reuse the same context. Counts stay zero on the
		// post run because they are baseline values, not live
		// snapshots.
		out.Plugins = baseline.Plugins
		out.CaptivePortalZones = baseline.CaptivePortalZones
		out.VtnetIndices = baseline.VtnetIndices
		out.InterfacesSet = baseline.InterfacesSet
	}
	return out, nil
}

// DefaultChecks returns the matrix's default check list. The
// ordering is stable so JSON output is deterministic.
func DefaultChecks(cfg Config) []Check {
	checks := []Check{
		// Routing.
		NewBGPv4NeighborCheck(cfg.BGPv4Neighbors),
		NewBGPv6NeighborCheck(cfg.BGPv6Neighbors),
		NewBGPDefaultV4Check(),
		NewBGPDefaultV6Check(),
		NewKernelDefaultV4Check(),
		NewKernelDefaultV6Check(),
		NewNAT44EgressCheck(),
		NewNAT64V6OnlyCheck(""),
		NewOutboundNATRulesCheck(2),
		NewWireguardHandshakeCheck(0),

		// DNS and DHCP.
		NewDNSResolveExternalCheck(cfg.OPNsenseLAN, ""),
		NewDNSResolveInternalCheck(cfg.OPNsenseLAN, ""),
		NewUnboundRunningCheck(),
		NewDHCPv4LeaseCountCheck(),
		NewDHCPv6IANALeaseCountCheck(),
		NewDHCPv6IAPDLeaseCountCheck(),
		NewRadvdAnnouncingCheck(),

		// Firewall.
		NewPFEnabledCheck(),
		NewPFRuleCountCheck(),
		NewPFNatRuleCountCheck(),
		NewPFStateTableGrowingCheck(0),
		NewCoreCaptiveportalZonesCheck(),
		NewCoreCaptiveportalAliasesCheck(),

		// MWAN integration.
		NewMWANOpnsenseDaemonRunningCheck(),
		NewMWANOpnsenseGRPCRespondingCheck(cfg.MWANOpnsenseSocket),
		NewQGAChannelResponsiveCheck(cfg.VMID),
		NewMWANOpnsenseHostSocketCheck(cfg.MWANOpnsenseHostSocket),

		// Web/API.
		NewGUIHTTPSRespondsCheck(),
		NewAPIFirmwareStatusCheck(cfg.APIAuth),
		NewAPIFirmwareVersionCheck(cfg.APIAuth, "26."),
		NewAPIAuthRejectsBadCredsCheck(),
		NewQuaggaAPIPostOnlyCheck(cfg.APIAuth),

		// Monitoring.
		NewWatchdogPathHealthyCheck(),
		NewNotifyEmailPathIntactCheck(),

		// Kernel/driver defaults.
		NewVtnetHWLRODisabledCheck(),
		NewInterfacesSetUnchangedCheck(),

		// pkg audit (informational).
		NewPkgAuditCheck(),
	}
	for _, name := range pluginsForBaseline(cfg) {
		checks = append(checks,
			NewPluginInstalledCheck(name),
			NewPluginRunningCheck(name, serviceNameFor(name)),
			NewPluginVersionCheck(name),
		)
	}
	return checks
}

// pluginsForBaseline returns the list of os-* plugins to register
// checks for. The actual applies-when filter consults the baseline
// at run time. The list here is the union of plugins observed in
// production per resolved decision O-2.
func pluginsForBaseline(_ Config) []string {
	return []string{
		"os-frr",
		"os-tayga",
		"os-acme-client",
		"os-crowdsec",
		"os-git-backup",
		"os-nginx",
		"os-redis",
		"os-wireguard",
	}
}

// pluginServiceNames maps a plugin package name to the corresponding
// FreeBSD service name used by `service ... status`. A map sidesteps
// the "switch on bare string" lint and is the canonical lookup table.
var pluginServiceNames = map[string]string{
	"os-frr":         "frr",
	"os-tayga":       "tayga",
	"os-wireguard":   "wireguard",
	"os-acme-client": "acme-client",
	"os-crowdsec":    "crowdsec",
	"os-nginx":       "nginx",
	"os-redis":       "redis",
	"os-git-backup":  "git-backup",
}

// serviceNameFor maps a plugin package name to its FreeBSD service
// name. Plugins absent from pluginServiceNames fall through to their
// package name; that path is rare and useful for new entries before
// the table is updated.
func serviceNameFor(plugin string) string {
	if name, ok := pluginServiceNames[plugin]; ok {
		return name
	}
	return plugin
}

// filterBySeverity returns the subset of checks whose severity is
// at least as severe as the supplied filter. Used by the
// `--severity-filter blocker` CLI mode to skip everything below
// blocker.
func filterBySeverity(checks []Check, want Severity) []Check {
	out := make([]Check, 0, len(checks))
	for _, c := range checks {
		if severityRank(c.Severity()) >= severityRank(want) {
			out = append(out, c)
		}
	}
	return out
}

func severityRank(s Severity) int {
	switch s {
	case SeverityBlocker:
		return 3
	case SeverityRegression:
		return 2
	case SeverityAdvisory:
		return 1
	default:
		return 0
	}
}

// SortResultsByID sorts the results slice in place by check ID.
// The CLI calls this before printing so the output is stable.
func SortResultsByID(results []Result) {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].CheckID < results[j].CheckID
	})
}

// CountByOutcome returns a map of outcome -> count for a result
// list. The CLI prints the summary at the end of every run.
func CountByOutcome(results []Result) map[Outcome]int {
	counts := map[Outcome]int{}
	for _, r := range results {
		counts[r.Outcome]++
	}
	return counts
}
