package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Check IDs are exported so the diff layer (and external callers) can
// refer to them without typing the literal twice. The IDs match the
// matrix doc so a grep across the docs and the code lines up.
const (
	CheckIDBGPv4NeighborEstablished     = "bgp_v4_neighbor_established"
	CheckIDBGPv6NeighborEstablished     = "bgp_v6_neighbor_established"
	CheckIDBGPDefaultV4Installed        = "bgp_default_v4_installed"
	CheckIDBGPDefaultV6Installed        = "bgp_default_v6_installed"
	CheckIDKernelDefaultV4Present       = "kernel_default_v4_present"
	CheckIDKernelDefaultV6Present       = "kernel_default_v6_present"
	CheckIDNAT44EgressWorks             = "nat44_egress_works"
	CheckIDNAT64V6OnlyToV4Works         = "nat64_v6_only_to_v4_works"
	CheckIDOutboundNATRulesLoaded       = "outbound_nat_rules_loaded"
	CheckIDWireguardHandshakeRecent     = "wireguard_handshake_recent"
	CheckIDKernelDefaultV4PersistsFinal = "kernel_default_v4_persists_post_finalize"
	CheckIDKernelDefaultV6PersistsFinal = "kernel_default_v6_persists_post_finalize"
)

// bgpSummaryJSON mirrors the relevant fields of the FRR `vtysh -c
// 'show bgp ipvN unicast summary json'` output.
type bgpSummaryJSON struct {
	Peers map[string]struct {
		State string `json:"state"`
	} `json:"peers"`
}

// runOPNsenseCommand is a tiny helper to record duration and
// stdout/stderr/exit on the Result. Every check uses it so the
// shape stays uniform.
func runOPNsenseCommand(
	ctx context.Context,
	env Env,
	command string,
	id string,
	cat Category,
	sev Severity,
) (Result, CommandResult, bool) {
	started := env.Now()
	cmd, err := env.SSHOPNsense(ctx, command)
	finished := env.Now()
	res := newResult(id, cat, sev, started, finished)
	res.RawStdout = cmd.Stdout
	res.RawExitCode = cmd.ExitCode
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = "ssh transport error: " + err.Error()
		return res, cmd, false
	}
	return res, cmd, true
}

// bgpNeighborEstablishedCheck wraps both v4 and v6 neighbor checks;
// only the family and ID change so they share an implementation.
type bgpNeighborEstablishedCheck struct {
	id       string
	family   string
	expected []string
}

// NewBGPv4NeighborCheck returns the v4 neighbor check.
//
// expected is the operator-supplied list of neighbor addresses
// (sourced from production.toml.j2 in real deployments). The check
// fails if any expected address is missing or not in Established.
func NewBGPv4NeighborCheck(expected []string) Check {
	return &bgpNeighborEstablishedCheck{
		id:       CheckIDBGPv4NeighborEstablished,
		family:   "ipv4",
		expected: expected,
	}
}

// NewBGPv6NeighborCheck returns the v6 neighbor check.
func NewBGPv6NeighborCheck(expected []string) Check {
	return &bgpNeighborEstablishedCheck{
		id:       CheckIDBGPv6NeighborEstablished,
		family:   "ipv6",
		expected: expected,
	}
}

func (c *bgpNeighborEstablishedCheck) ID() string         { return c.id }
func (c *bgpNeighborEstablishedCheck) Category() Category { return CategoryRouting }
func (c *bgpNeighborEstablishedCheck) Severity() Severity { return SeverityBlocker }

func (c *bgpNeighborEstablishedCheck) AppliesWhen(b *Baseline) bool {
	if b == nil {
		return true
	}
	return b.HasPlugin("os-frr")
}

func (c *bgpNeighborEstablishedCheck) Run(ctx context.Context, env Env) Result {
	command := fmt.Sprintf("vtysh -c 'show bgp %s unicast summary json'", c.family)
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.id, c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode != 0 {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("vtysh exit %d", cmd.ExitCode)
		return res
	}
	var summary bgpSummaryJSON
	if err := json.Unmarshal([]byte(cmd.Stdout), &summary); err != nil {
		res.Outcome = OutcomeError
		res.Message = fmt.Sprintf("decode bgp summary json: %v", err)
		return res
	}
	missing := []string{}
	notEstablished := []string{}
	for _, addr := range c.expected {
		peer, found := summary.Peers[addr]
		if !found {
			missing = append(missing, addr)
			continue
		}
		if peer.State != "Established" {
			notEstablished = append(notEstablished, addr+"="+peer.State)
		}
	}
	if len(missing) == 0 && len(notEstablished) == 0 {
		res.Outcome = OutcomePass
		res.ParsedValue = "all_established"
		return res
	}
	res.Outcome = OutcomeFail
	res.ParsedValue = "not_all_established"
	res.Message = fmt.Sprintf(
		"missing=%s not_established=%s",
		strings.Join(missing, ","),
		strings.Join(notEstablished, ","),
	)
	return res
}

// bgpRouteJSON is the relevant slice of `vtysh -c 'show ip[v6] route ::/0 json'`.
// FRR returns a map of prefix-> array of route entries.
type bgpRouteJSON map[string][]struct {
	Protocol  string `json:"protocol"`
	Installed bool   `json:"installed"`
	Selected  bool   `json:"selected"`
}

// bgpDefaultRouteCheck checks whether a BGP-installed default route
// exists in FRR's RIB.
type bgpDefaultRouteCheck struct {
	id     string
	family string
	prefix string
}

// NewBGPDefaultV4Check returns the v4 default-route check.
func NewBGPDefaultV4Check() Check {
	return &bgpDefaultRouteCheck{
		id:     CheckIDBGPDefaultV4Installed,
		family: "ip",
		prefix: "0.0.0.0/0",
	}
}

// NewBGPDefaultV6Check returns the v6 default-route check.
func NewBGPDefaultV6Check() Check {
	return &bgpDefaultRouteCheck{
		id:     CheckIDBGPDefaultV6Installed,
		family: "ipv6",
		prefix: "::/0",
	}
}

func (c *bgpDefaultRouteCheck) ID() string         { return c.id }
func (c *bgpDefaultRouteCheck) Category() Category { return CategoryRouting }
func (c *bgpDefaultRouteCheck) Severity() Severity { return SeverityBlocker }
func (c *bgpDefaultRouteCheck) AppliesWhen(b *Baseline) bool {
	return b == nil || b.HasPlugin("os-frr")
}

func (c *bgpDefaultRouteCheck) Run(ctx context.Context, env Env) Result {
	command := fmt.Sprintf("vtysh -c 'show %s route %s json'", c.family, c.prefix)
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.id, c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode != 0 {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("vtysh exit %d", cmd.ExitCode)
		return res
	}
	var routes bgpRouteJSON
	if err := json.Unmarshal([]byte(cmd.Stdout), &routes); err != nil {
		res.Outcome = OutcomeError
		res.Message = fmt.Sprintf("decode route json: %v", err)
		return res
	}
	bgpInstalled := 0
	for _, entries := range routes {
		for _, e := range entries {
			if e.Protocol == "bgp" && e.Installed && e.Selected {
				bgpInstalled++
			}
		}
	}
	if bgpInstalled == 1 {
		res.Outcome = OutcomePass
		res.ParsedValue = "bgp_default_installed"
		return res
	}
	res.Outcome = OutcomeFail
	res.ParsedValue = fmt.Sprintf("bgp_installed=%d", bgpInstalled)
	res.Message = "expected exactly one BGP-selected default route"
	return res
}

// kernelDefaultRouteCheck checks the FreeBSD kernel routing table
// for a default route.
type kernelDefaultRouteCheck struct {
	id     string
	family string
}

// NewKernelDefaultV4Check returns the kernel v4 default check.
func NewKernelDefaultV4Check() Check {
	return &kernelDefaultRouteCheck{id: CheckIDKernelDefaultV4Present, family: "inet"}
}

// NewKernelDefaultV6Check returns the kernel v6 default check.
func NewKernelDefaultV6Check() Check {
	return &kernelDefaultRouteCheck{id: CheckIDKernelDefaultV6Present, family: "inet6"}
}

func (c *kernelDefaultRouteCheck) ID() string                   { return c.id }
func (c *kernelDefaultRouteCheck) Category() Category           { return CategoryRouting }
func (c *kernelDefaultRouteCheck) Severity() Severity           { return SeverityBlocker }
func (c *kernelDefaultRouteCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *kernelDefaultRouteCheck) Run(ctx context.Context, env Env) Result {
	command := fmt.Sprintf(`netstat -rn -f %s | awk '$1=="default"{print $2}'`, c.family)
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.id, c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode != 0 {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("netstat exit %d", cmd.ExitCode)
		return res
	}
	gw := strings.TrimSpace(cmd.Stdout)
	if gw == "" {
		res.Outcome = OutcomeFail
		res.ParsedValue = ""
		res.Message = "no default route in kernel"
		return res
	}
	res.Outcome = OutcomePass
	res.ParsedValue = gw
	return res
}

// nat44EgressCheck verifies that a LAN client can fetch over IPv4.
// The pass criterion is exit code 0 and an HTTP 200 response.
type nat44EgressCheck struct{}

// NewNAT44EgressCheck returns the v4 egress data-plane check.
func NewNAT44EgressCheck() Check {
	return &nat44EgressCheck{}
}

func (c *nat44EgressCheck) ID() string                   { return CheckIDNAT44EgressWorks }
func (c *nat44EgressCheck) Category() Category           { return CategoryRouting }
func (c *nat44EgressCheck) Severity() Severity           { return SeverityBlocker }
func (c *nat44EgressCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *nat44EgressCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	out, err := env.LANClientExec(
		ctx,
		`curl -4 -m 5 -o /dev/null -w '%{http_code}' http://ifconfig.co/ip`,
	)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	res.RawStdout = out.Stdout
	res.RawExitCode = out.ExitCode
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	code := strings.TrimSpace(out.Stdout)
	res.ParsedValue = code
	if out.ExitCode == 0 && code == "200" {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("curl exit=%d code=%q", out.ExitCode, code)
	return res
}

// nat64V6OnlyCheck pings a synthesised AAAA via Tayga.
type nat64V6OnlyCheck struct {
	target string
}

// NewNAT64V6OnlyCheck returns the NAT64 data-plane check. The
// target is the v6 prefix-mapped representation of an external v4
// address (defaults to 64:ff9b::1.1.1.1).
func NewNAT64V6OnlyCheck(target string) Check {
	if target == "" {
		target = "64:ff9b::1.1.1.1"
	}
	return &nat64V6OnlyCheck{target: target}
}

func (c *nat64V6OnlyCheck) ID() string         { return CheckIDNAT64V6OnlyToV4Works }
func (c *nat64V6OnlyCheck) Category() Category { return CategoryRouting }
func (c *nat64V6OnlyCheck) Severity() Severity { return SeverityRegression }

func (c *nat64V6OnlyCheck) AppliesWhen(b *Baseline) bool {
	if b == nil {
		return true
	}
	return b.HasPlugin("os-tayga")
}

func (c *nat64V6OnlyCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	command := "ping6 -c 1 -s 16 " + c.target
	out, err := env.LANClientExec(ctx, command)
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
		res.ParsedValue = "ping6_ok"
		return res
	}
	res.Outcome = OutcomeFail
	res.ParsedValue = "ping6_fail"
	res.Message = fmt.Sprintf("ping6 exit=%d", out.ExitCode)
	return res
}

// outboundNATRulesCheck counts pf NAT rules.
type outboundNATRulesCheck struct {
	minRules int
}

// NewOutboundNATRulesCheck returns the pf NAT rule-count check
// (default minimum 2 per the matrix).
func NewOutboundNATRulesCheck(minRules int) Check {
	if minRules <= 0 {
		minRules = 2
	}
	return &outboundNATRulesCheck{minRules: minRules}
}

func (c *outboundNATRulesCheck) ID() string                   { return CheckIDOutboundNATRulesLoaded }
func (c *outboundNATRulesCheck) Category() Category           { return CategoryRouting }
func (c *outboundNATRulesCheck) Severity() Severity           { return SeverityBlocker }
func (c *outboundNATRulesCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *outboundNATRulesCheck) Run(ctx context.Context, env Env) Result {
	command := `pfctl -sn | grep -c 'nat on '`
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	count := parseIntOrZero(strings.TrimSpace(cmd.Stdout))
	res.ParsedValue = strconv.Itoa(count)
	if count >= c.minRules {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("nat rule count %d below minimum %d", count, c.minRules)
	return res
}

// wireguardHandshakeCheck verifies every WireGuard peer's last
// handshake is recent.
type wireguardHandshakeCheck struct {
	maxAge time.Duration
}

// NewWireguardHandshakeCheck returns the WG check; maxAge defaults
// to 3 * keepalive (90s) per the matrix.
func NewWireguardHandshakeCheck(maxAge time.Duration) Check {
	if maxAge <= 0 {
		maxAge = 90 * time.Second
	}
	return &wireguardHandshakeCheck{maxAge: maxAge}
}

func (c *wireguardHandshakeCheck) ID() string         { return CheckIDWireguardHandshakeRecent }
func (c *wireguardHandshakeCheck) Category() Category { return CategoryRouting }
func (c *wireguardHandshakeCheck) Severity() Severity { return SeverityRegression }

func (c *wireguardHandshakeCheck) AppliesWhen(b *Baseline) bool {
	if b == nil {
		return true
	}
	return b.HasPlugin("os-wireguard")
}

func (c *wireguardHandshakeCheck) Run(ctx context.Context, env Env) Result {
	command := "wg show all latest-handshakes"
	res, cmd, ok := runOPNsenseCommand(ctx, env, command, c.ID(), c.Category(), c.Severity())
	if !ok {
		return res
	}
	if cmd.ExitCode != 0 {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("wg exit %d", cmd.ExitCode)
		return res
	}
	now := env.Now().Unix()
	stale := []string{}
	peers := 0
	for line := range strings.SplitSeq(strings.TrimSpace(cmd.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		peers++
		ts := parseIntOrZero(fields[len(fields)-1])
		if ts == 0 {
			stale = append(stale, fields[1])
			continue
		}
		age := now - int64(ts)
		if age > int64(c.maxAge.Seconds()) {
			stale = append(stale, fields[1])
		}
	}
	res.ParsedValue = fmt.Sprintf("peers=%d stale=%d", peers, len(stale))
	if len(stale) == 0 {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = "stale peers: " + strings.Join(stale, ",")
	return res
}

func parseIntOrZero(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
