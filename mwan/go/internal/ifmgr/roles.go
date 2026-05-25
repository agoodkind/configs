//go:build linux

package ifmgr

import "fmt"

// roleModules maps each known role to the ordered list of module names
// that should run for it. Order matters: modules execute in the listed
// sequence on each Reconcile pass. Choose order so that a module's
// preconditions are likely to be in place by the time it runs (e.g.
// policy_rules before oobv6, since oobv6 may already need egress to
// solicit RA from the OOB tunnel side).
//
// Adding a new role: append the entry here and ensure the named modules
// are registered (their package is imported by main.go).
var roleModules = map[string][]string{
	"vault-oob": {
		"policy_rules",
		"oobv6",
		"oobv4",
		"ra_lost",
		// cloudflared_tap is a log forwarder. It tails a configured
		// systemd unit (cloudflared-oob) and re-emits each entry through
		// the daemon's slog logger so cloudflared events flow through
		// the same JSON log file and email pipeline as everything else.
		// Pure log forwarder: no kernel state.
		"cloudflared_tap",
		// wg_health polls a remote WireGuard server (typically OPNsense)
		// over SSH and alerts when any peer's handshake age crosses
		// configured thresholds. Read-only observer; no kernel state.
		"wg_health",
	},
	// lxc-failover-backup is the iface-monitor role for prod LXC 116 and
	// testbed LXC 117. mainv4 is included so that when dhcp_v4 is enabled
	// for the iface, the daemon's DHCP client also drives kernel addr and
	// the main-table default route. If dhcp_v4 is disabled, mainv4's Init
	// returns an error and the daemon falls back to the no-mainv4 modules
	// (this is intentional; prod LXC 116 today runs without mainv4).
	"lxc-failover-backup": {
		"slaac_health",
		"bridge_probe",
		"connectivity_probe",
		"ra_lost",
		"mainv4",
	},
	// suburban-wg is the WG-endpoint observer role for the suburban testbed
	// host. It runs wg_health in local-exec mode (ssh_host empty) so the
	// daemon polls the local wg0 and emits suburban's own view of every
	// peer endpoint. Combined with vault's wg_health (OPNsense view),
	// log analysis can surface split-brain (each side believes a different
	// peer endpoint) caused by WG roaming + asymmetric NAT paths.
	// See mwan/WIREGUARD-ROAMING-NOTES.md for protocol details.
	"suburban-wg": {
		"wg_health",
	},
	// suburban-oob is the testbed analog of vault-oob. It selects the same
	// kernel-state modules so the suburban testbed VM exercises the same
	// reconcile paths as vault, against the simulated mbrains ISP iface
	// (enmbrains0) and a testbed-only OOB v6 prefix under
	// 3d06:bad:b01:2ff::/64. host_ipv6_policy keeps the hypervisor's own
	// bridge RA policy aligned with the intended uplink and simulated-ISP
	// roles before the OOB modules rely on stable IPv6 egress. cloudflared_tap
	// and wg_health are excluded because the testbed has no cloudflared-oob
	// tunnel and no remote WG peer to observe through ifmgr.
	"suburban-oob": {
		"policy_rules",
		"host_ipv6_policy",
		"oobv6",
		"oobv4",
		"ra_lost",
	},
}

// modulesForRole returns the module name list for the named role, or an
// error if the role is not known.
func modulesForRole(role string) ([]string, error) {
	names, ok := roleModules[role]
	if !ok {
		valid := make([]string, 0, len(roleModules))
		for k := range roleModules {
			valid = append(valid, k)
		}
		return nil, fmt.Errorf("unknown ifmgr role %q (valid: %v)", role, valid)
	}
	return names, nil
}

// KnownRoles returns the sorted list of role names this binary supports.
// Used by the CLI --help and config validation.
func KnownRoles() []string {
	out := make([]string, 0, len(roleModules))
	for k := range roleModules {
		out = append(out, k)
	}
	return out
}
