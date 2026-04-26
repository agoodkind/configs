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
	},
	"lxc-failover-backup": {
		"slaac_health",
		"bridge_probe",
		"connectivity_probe",
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
