//go:build linux

// Package wanconfig projects the configuration the gateway daemon loaded onto
// the model it describes itself with, so the management surface serves what
// the running process holds rather than a file on disk. The projection turns
// a Gateway value into the path-value items the publishing binding writes.
//
// The package carries the platform constraint of the gateway it describes:
// only the linux daemon loads this configuration and only its sysrepo
// binding publishes it, so the freebsd router build never reaches this code.
package wanconfig

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
)

// Item is one path-value pair in the published tree. It mirrors the
// publishing binding's item so the projection can be built and tested on
// every platform; the linux publisher converts it on the way out.
type Item struct {
	Path  string
	Value string
}

// Member is one steering member exactly as the daemon loaded it: the link
// that carries it, the tier the router assigns it, the probe policy that
// decides its health, and the prefix-translation pair it carries when its
// configuration names one.
type Member struct {
	// Name is the member's stable name (att, webpass, monkeybrains). It
	// names the probe policy and the translation instance.
	Name string
	// Iface is the link the member rides. It is the interface entry's key.
	Iface string
	// Tier is the steering tier the router assigns; lower is preferred.
	Tier uint8
	// ProbePolicy is the name of the health policy that decides the member's
	// state, or empty when the daemon runs no probe for it.
	ProbePolicy string
	// NPTInternal and NPTExternal are the prefix-translation pair the member
	// carries. Both are zero when the configuration names none; an instance
	// is published only when both are set.
	NPTInternal netip.Prefix
	NPTExternal netip.Prefix
}

// WatchdogSettings is the rollback watchdog's policy as the publishing
// daemon's configuration carries it: the thresholds that decide when a
// deploy is judged and rolled back, and the targets its probes exercise.
// Present gates publishing, so a host whose configuration carries no
// watchdog section publishes nothing under the container.
type WatchdogSettings struct {
	Present                      bool
	DeployWindowMinutes          uint16
	ConnectivityTimeoutSeconds   uint16
	CheckIntervalHealthySeconds  uint16
	CheckIntervalDegradedSeconds uint16
	PostRollbackGraceSeconds     uint16
	AlertCooldownSeconds         uint16
	DeployGracePeriodSeconds     uint16
	MaxRollbackAttempts          uint8
	SnapshotHealthyThreshold     uint16
	MaxKnownGoodSnapshots        uint8
	// PingTargets are the addresses the watchdog pings, both families.
	PingTargets []netip.Addr
}

// OOBSettings is the out-of-band access policy as the publishing daemon's
// configuration carries it. Each family publishes only when its module
// config is present in the daemon's role.
type OOBSettings struct {
	V6Present         bool
	V6Iface           string
	V6Addr            netip.Addr
	V6TableID         uint32
	ManageSLAACRule   bool
	SLAACRulePriority uint32
	V4Present         bool
	V4Iface           string
	V4TableID         uint32
}

// TapSettings is the tunnel tap as the publishing daemon's configuration
// carries it: the unit whose journal is tailed and the patterns whose
// entries are downgraded.
type TapSettings struct {
	Present           bool
	Unit              string
	DowngradePatterns []string
}

// DaemonSettings are the daemon settings no published model covers,
// published under the local module's daemon container.
type DaemonSettings struct {
	Watchdog WatchdogSettings
	OOB      OOBSettings
	Tap      TapSettings
}

// Gateway is the loaded configuration the surface publishes.
type Gateway struct {
	// InternalIface is the link toward the router behind the gateway. It is
	// published as an interface entry with both families enabled, because
	// the daemon routes both families over it.
	InternalIface string
	// Members are the steering members in a stable order.
	Members []Member
	// Daemon carries the settings published under the local module's
	// daemon container.
	Daemon DaemonSettings
}

// OwnedPaths are the subtrees the daemon owns in the configuration
// datastore. A publish replaces them wholesale, so an entry a retired
// configuration wrote cannot survive a restart.
var OwnedPaths = []string{
	interfacesPath,
	natPath,
	daemonPath,
}

const (
	interfacesPath = "/ietf-interfaces:interfaces"
	natPath        = "/ietf-nat:nat"
	daemonPath     = "/goodkind-mwan-steering:daemon"

	// ifTypeUnspecified is the interface type published for every entry: the
	// daemon's configuration carries no link type, and the model requires
	// one, so the entry says so rather than guessing. The live-state piece
	// reports the kernel's type in the operational datastore.
	ifTypeUnspecified = "iana-if-type:other"

	// natTypeNPTv6 is the translation type of every instance this
	// configuration can express today.
	natTypeNPTv6 = "ietf-nat:nptv6"

	// natPolicyID is the single policy each instance carries.
	natPolicyID = 1

	boolTrue = "true"
)

// ErrInvalidGateway means the Gateway cannot be published as given. The
// message names the member and field.
var ErrInvalidGateway = errors.New("wanconfig: invalid gateway")

// ConfigItems returns the items that describe g in the model, in a stable
// order, or an error when g cannot be published faithfully: a member or the
// internal link with no name, a name that cannot be quoted into a path, a
// duplicate link, or a translation pair with only one prefix.
func ConfigItems(g Gateway) ([]Item, error) {
	if err := validate(g); err != nil {
		return nil, err
	}

	items := make([]Item, 0, 8*len(g.Members)+4)
	items = append(items, interfaceItems(g.InternalIface)...)
	for _, member := range g.Members {
		items = append(items, interfaceItems(member.Iface)...)
		items = append(items, steeringItems(member)...)
	}
	instanceID := uint32(0)
	for _, member := range g.Members {
		if !member.NPTInternal.IsValid() {
			continue
		}
		instanceID++
		items = append(items, natInstanceItems(instanceID, member)...)
	}
	items = append(items, daemonItems(g.Daemon)...)
	return items, nil
}

// daemonItems describes the daemon settings the loaded configuration
// carries. A section that is not present publishes nothing, so the tree
// never invents values another host owns.
func daemonItems(daemon DaemonSettings) []Item {
	items := make([]Item, 0, 16)
	items = append(items, watchdogItems(daemon.Watchdog)...)
	items = append(items, oobItems(daemon.OOB)...)
	items = append(items, tapItems(daemon.Tap)...)
	return items
}

func watchdogItems(watchdog WatchdogSettings) []Item {
	if !watchdog.Present {
		return nil
	}
	base := daemonPath + "/watchdog"
	items := []Item{
		{Path: base + "/deploy-window-minutes", Value: uintValue(uint64(watchdog.DeployWindowMinutes))},
		{Path: base + "/connectivity-timeout-seconds", Value: uintValue(uint64(watchdog.ConnectivityTimeoutSeconds))},
		{Path: base + "/check-interval-healthy-seconds", Value: uintValue(uint64(watchdog.CheckIntervalHealthySeconds))},
		{Path: base + "/check-interval-degraded-seconds", Value: uintValue(uint64(watchdog.CheckIntervalDegradedSeconds))},
		{Path: base + "/post-rollback-grace-seconds", Value: uintValue(uint64(watchdog.PostRollbackGraceSeconds))},
		{Path: base + "/alert-cooldown-seconds", Value: uintValue(uint64(watchdog.AlertCooldownSeconds))},
		{Path: base + "/deploy-grace-period-seconds", Value: uintValue(uint64(watchdog.DeployGracePeriodSeconds))},
		{Path: base + "/max-rollback-attempts", Value: uintValue(uint64(watchdog.MaxRollbackAttempts))},
		{Path: base + "/snapshot-healthy-threshold", Value: uintValue(uint64(watchdog.SnapshotHealthyThreshold))},
		{Path: base + "/max-known-good-snapshots", Value: uintValue(uint64(watchdog.MaxKnownGoodSnapshots))},
	}
	// Leaf-list entries are addressed by their value, not a predicate, so
	// the datastore keys each instance by the value itself.
	for _, target := range watchdog.PingTargets {
		items = append(items, Item{
			Path:  base + "/probe-targets/ping",
			Value: target.String(),
		})
	}
	return items
}

func oobItems(oob OOBSettings) []Item {
	items := make([]Item, 0, 7)
	if oob.V6Present {
		base := daemonPath + "/oob/ipv6"
		items = append(items,
			Item{Path: base + "/interface", Value: oob.V6Iface},
			Item{Path: base + "/table-id", Value: uintValue(uint64(oob.V6TableID))},
			Item{Path: base + "/manage-slaac-rule", Value: boolValue(oob.ManageSLAACRule)},
			Item{Path: base + "/slaac-rule-priority", Value: uintValue(uint64(oob.SLAACRulePriority))},
		)
		// The address leaf is typed; a configuration that carries none
		// publishes none rather than an unparsable value.
		if oob.V6Addr.IsValid() {
			items = append(items, Item{Path: base + "/address", Value: oob.V6Addr.String()})
		}
	}
	if oob.V4Present {
		base := daemonPath + "/oob/ipv4"
		items = append(items,
			Item{Path: base + "/interface", Value: oob.V4Iface},
			Item{Path: base + "/table-id", Value: uintValue(uint64(oob.V4TableID))},
		)
	}
	return items
}

func tapItems(tap TapSettings) []Item {
	if !tap.Present {
		return nil
	}
	base := daemonPath + "/tap"
	items := []Item{{Path: base + "/unit", Value: tap.Unit}}
	// Patterns carry regex text a path predicate cannot quote, so each
	// leaf-list entry is addressed by the bare path with its value.
	for _, pattern := range tap.DowngradePatterns {
		items = append(items, Item{
			Path:  base + "/downgrade-pattern",
			Value: pattern,
		})
	}
	return items
}

// uintValue renders one unsigned leaf value.
func uintValue(value uint64) string {
	return strconv.FormatUint(value, 10)
}

// boolValue renders one boolean leaf value.
func boolValue(value bool) string {
	if value {
		return boolTrue
	}
	return "false"
}

// interfaceItems describes one link: the entry exists, its type is
// unspecified, it is enabled, and both address families are enabled.
func interfaceItems(iface string) []Item {
	base := interfacePath(iface)
	return []Item{
		{Path: base + "/type", Value: ifTypeUnspecified},
		{Path: base + "/enabled", Value: boolTrue},
		{Path: base + "/ietf-ip:ipv4/enabled", Value: boolTrue},
		{Path: base + "/ietf-ip:ipv6/enabled", Value: boolTrue},
	}
}

// steeringItems marks the member's link as a steering member with its tier
// and, when the daemon probes it, the probe policy that decides its state.
func steeringItems(member Member) []Item {
	base := interfacePath(member.Iface) + "/goodkind-mwan-steering:steering"
	items := []Item{
		{Path: base + "/tier", Value: strconv.FormatUint(uint64(member.Tier), 10)},
	}
	if member.ProbePolicy != "" {
		items = append(items, Item{Path: base + "/probe-policy", Value: member.ProbePolicy})
	}
	return items
}

// natInstanceItems describes the member's prefix-translation instance with
// both prefixes explicit.
func natInstanceItems(id uint32, member Member) []Item {
	base := natPath + "/instances/instance[id='" + strconv.FormatUint(uint64(id), 10) + "']"
	prefixes := base + "/policy[id='" + strconv.Itoa(natPolicyID) + "']" +
		"/nptv6-prefixes[internal-ipv6-prefix='" + member.NPTInternal.String() + "']"
	return []Item{
		{Path: base + "/name", Value: member.Name},
		{Path: base + "/type", Value: natTypeNPTv6},
		{Path: base + "/enable", Value: boolTrue},
		{Path: prefixes + "/external-ipv6-prefix", Value: member.NPTExternal.String()},
	}
}

// interfacePath is the list entry for one link.
func interfacePath(iface string) string {
	return interfacesPath + "/interface[name='" + iface + "']"
}

// invalid logs and returns one rejection, so every validation failure is
// both visible in the daemon log and matchable with ErrInvalidGateway.
func invalid(reason string) error {
	slog.Warn("wanconfig: invalid gateway", "reason", reason)
	return fmt.Errorf("%w: %s", ErrInvalidGateway, reason)
}

// validate rejects a Gateway the projection cannot express as paths.
func validate(g Gateway) error {
	if err := validateKey("internal link", g.InternalIface); err != nil {
		return err
	}
	seen := map[string]string{g.InternalIface: "internal link"}
	for _, member := range g.Members {
		if err := validateKey("member name", member.Name); err != nil {
			return err
		}
		if err := validateKey("member "+member.Name+" link", member.Iface); err != nil {
			return err
		}
		if owner, dup := seen[member.Iface]; dup {
			return invalid(fmt.Sprintf("member %s link %q is already the %s", member.Name, member.Iface, owner))
		}
		seen[member.Iface] = "member " + member.Name
		if member.NPTInternal.IsValid() != member.NPTExternal.IsValid() {
			return invalid(fmt.Sprintf("member %s translation needs both prefixes (internal %q, external %q)",
				member.Name, member.NPTInternal, member.NPTExternal))
		}
		if member.NPTInternal.IsValid() && (!member.NPTInternal.Addr().Is6() || !member.NPTExternal.Addr().Is6()) {
			return invalid(fmt.Sprintf("member %s translation prefixes must be IPv6 (internal %q, external %q)",
				member.Name, member.NPTInternal, member.NPTExternal))
		}
	}
	return nil
}

// validateKey rejects an empty key or one that cannot sit inside the
// single-quoted predicate the paths use.
func validateKey(what string, value string) error {
	if value == "" {
		return invalid(what + " is empty")
	}
	if strings.ContainsAny(value, "'\"[]/") {
		return invalid(fmt.Sprintf("%s %q contains a character a path cannot carry", what, value))
	}
	return nil
}
