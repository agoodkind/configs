# Multiple downstream routers over BGP: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** MWAN learns each downstream router's LAN prefixes over iBGP and installs them as kernel return routes, so adding router N+1 is config plus a deploy.

**Architecture:** Config declares each router (transit addresses plus allowed v6 allocations). The agent's embedded GoBGP speaker peers with every router, filters announcements to each router's allocation, and installs accepted best paths into the main table and every WAN table, tagged proto bgp. The wan.routes static internal route retires behind a shadow flag. The spec is [spec.md](spec.md).

**Tech Stack:** Go (gobgp v4, vishvananda/netlink via internal/netif), Ansible group vars and Jinja templates, FRR on the testbed router-2 simulator.

## Global Constraints

- Every Go slice must pass the full acceptance gate end to end before it commits: `make -C mwan/go build`. That target runs `proto`, then `build-linux` and `build-mwan-opnsense`, each of which depends on `check` (build, vet, lint, test, govulncheck, staticcheck-extra). It is stronger than `check` alone because it cross-compiles the FreeBSD binary, so a linux-only file missing its non-linux stub fails here rather than on a target host. Never run raw `go`.
- Single TOML config: every new knob lives in `/etc/mwan/config.toml`, rendered from group vars. No env-var config.
- No `| default()` or `is defined` on Ansible input variables; `configs lint` enforces.
- iBGP AS is `4200000001`. Learned kernel routes carry protocol `unix.RTPROT_BGP` (186).
- Allocations are v6 only, must be disjoint across routers, and must sit inside the internal block (`3d06:bad:b01::/60` production, the testbed twin in its group vars).
- Shadow-first: both new shadow flags default to shadow on every host; authoritative flips are separate, operator-approved deploys, testbed before production.
- Files stay under 500 lines; split by responsibility.
- Commits are signed (`git commit -S`) with the `Co-authored-by: Claude <noreply@anthropic.com>` trailer.

---

### Task 1: Router entries in the TOML config

**Files:**
- Modify: `mwan/go/internal/config/config.go` (BGPSection, near line 293)
- Test: `mwan/go/internal/config/bgp_routers_test.go` (create)

**Interfaces:**
- Produces: `config.BGPRouter{Name, AddressV4, AddressV6 string, AllocationsV6 []string}`, `BGPSection.Routers []BGPRouter`, `BGPSection.RoutesShadowMode bool`, and `validateBGPRouters(routers []BGPRouter, internalBlock string) error`.
- Consumes: the existing `BGPSection` and the `[ifmgr] internal_prefix` value already loaded by the config package.

- [ ] **Step 1: Write the failing tests**

```go
func TestValidateBGPRoutersRejectsOverlap(t *testing.T) {
	routers := []BGPRouter{
		{Name: "opnsense", AddressV4: "10.250.250.2", AddressV6: "3d06:bad:b01:fe::2",
			AllocationsV6: []string{"3d06:bad:b01::/61"}},
		{Name: "router2", AddressV4: "10.250.250.5", AddressV6: "3d06:bad:b01:fe::5",
			AllocationsV6: []string{"3d06:bad:b01:4::/62"}},
	}
	err := validateBGPRouters(routers, "3d06:bad:b01::/60")
	if err == nil {
		t.Fatal("overlapping allocations must fail validation")
	}
}

func TestValidateBGPRoutersRejectsOutsideBlock(t *testing.T) {
	routers := []BGPRouter{{Name: "r", AddressV4: "10.250.250.5",
		AddressV6: "3d06:bad:b01:fe::5", AllocationsV6: []string{"2001:db8::/64"}}}
	if validateBGPRouters(routers, "3d06:bad:b01::/60") == nil {
		t.Fatal("allocation outside the internal block must fail")
	}
}

func TestValidateBGPRoutersAcceptsDisjointAndHost(t *testing.T) {
	routers := []BGPRouter{
		{Name: "a", AddressV4: "10.250.250.2", AddressV6: "3d06:bad:b01:fe::2",
			AllocationsV6: []string{"3d06:bad:b01::/61"}},
		{Name: "b", AddressV4: "10.250.250.5", AddressV6: "3d06:bad:b01:fe::5",
			AllocationsV6: []string{"3d06:bad:b01:8::1/128"}},
	}
	if err := validateBGPRouters(routers, "3d06:bad:b01::/60"); err != nil {
		t.Fatalf("disjoint allocations must pass: %v", err)
	}
}
```

Also assert missing `Name`, missing addresses, and duplicate names fail.

- [ ] **Step 2: Run and verify failure**

Run: `make -C mwan/go build`

- [ ] **Step 3: Implement**

Add to `BGPSection`: `Routers []BGPRouter` (`toml:"routers"`), `RoutesShadowMode bool` (`toml:"routes_shadow_mode"`). Add `BGPRouter` with `netip.ParsePrefix`-based validation: every field required, names unique, each allocation inside the internal block, pairwise disjointness via `Prefix.Overlaps`. Call `validateBGPRouters` from the existing BGP validation path, passing the loaded internal prefix; skip when `Routers` is empty so the failover LXC config without an `[ifmgr.wan]` block still loads.

- [ ] **Step 4: Verify pass**

Run: `make -C mwan/go build`

- [ ] **Step 5: Commit**

```bash
git add mwan/go/internal/config/config.go mwan/go/internal/config/bgp_routers_test.go
git commit -S -m "Add BGP router entries with allocation validation to the config schema" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task 2: Speaker peers from router entries

**Files:**
- Modify: `mwan/go/internal/bgp/config.go`, `mwan/go/internal/bgp/speaker.go`
- Modify: `mwan/go/internal/agent/main.go` (the `bgp.Config` build near line 80)
- Test: `mwan/go/internal/bgp/speaker_test.go`

**Interfaces:**
- Produces: `bgp.Router{Name string, AddressV4, AddressV6 string, AllocationsV6 []netip.Prefix}` and `bgp.Config.Routers []Router`.
- Consumes: Task 1's `config.BGPRouter`; the agent maps it field-for-field and parses allocations with `netip.ParsePrefix` (parse errors are fatal at startup, already validated at load).

- [ ] **Step 1: Write the failing test**

Extend the `fakeBGPServer` pattern already in `speaker_test.go`:

```go
func TestStartAddsPeersPerRouterBothFamilies(t *testing.T) {
	fake := newFakeBGPServer()
	cfg := baseGRConfig()
	cfg.Neighbors, cfg.NeighborsV6 = nil, nil
	cfg.Routers = []Router{
		{Name: "opnsense", AddressV4: "10.250.250.2", AddressV6: "3d06:bad:b01:fe::2"},
		{Name: "router2", AddressV4: "10.250.250.5", AddressV6: "3d06:bad:b01:fe::5"},
	}
	s := newSpeakerWithFake(cfg, fake)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(fake.addPeerReqs); got != 4 {
		t.Fatalf("addPeer calls = %d, want 4 (v4+v6 per router)", got)
	}
}
```

- [ ] **Step 2: Run and verify failure** (`make -C mwan/go build`; expect `Routers` undefined).

- [ ] **Step 3: Implement**

Add `Router` to `bgp/config.go`. In `Speaker.Start`, when `cfg.Routers` is non-empty, loop routers calling the existing `addPeer(ctx, r.AddressV4, false)` and `addPeer(ctx, r.AddressV6, true)`; keep the legacy `Neighbors`/`NeighborsV6` loops for an empty router list so nothing breaks mid-migration. Add `routerForPeer(addr string) *Router` (exact match against either family address) for Tasks 3 and 4. In `agent/main.go`, map `cfg.BGP.Routers` into `bgpCfg.Routers`.

- [ ] **Step 4: Verify pass** (`make -C mwan/go build`).

- [ ] **Step 5: Commit**

```bash
git add mwan/go/internal/bgp/ mwan/go/internal/agent/main.go
git commit -S -m "Derive speaker peering from per-router config entries" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task 3: Allocation guard and undeclared-announcement alert

**Files:**
- Create: `mwan/go/internal/bgp/guard.go`
- Test: `mwan/go/internal/bgp/guard_test.go`

**Interfaces:**
- Produces: `func (s *Speaker) AllowedPath(peerAddr string, prefix netip.Prefix) bool` (pure allocation check) and `func (s *Speaker) AuditAdjRibIn(ctx context.Context) []Violation` where `Violation{Router string, Prefix netip.Prefix}`.
- Consumes: Task 2's `Routers` and `routerForPeer`; the GoBGP `ListPath` API on adj-rib-in (add `ListPath` to the `bgpServerAPI` interface and the fake).

- [ ] **Step 1: Write the failing tests**

```go
func TestAllowedPathInsideAllocation(t *testing.T) {
	s := speakerWithRouters(t) // helper: router2 allocation 3d06:bad:b01:4::/62
	if !s.AllowedPath("3d06:bad:b01:fe::5", netip.MustParsePrefix("3d06:bad:b01:4::/64")) {
		t.Fatal("prefix inside allocation must be allowed")
	}
	if s.AllowedPath("3d06:bad:b01:fe::5", netip.MustParsePrefix("3d06:bad:b01::/64")) {
		t.Fatal("prefix outside allocation must be rejected")
	}
	if s.AllowedPath("3d06:bad:b01:fe::99", netip.MustParsePrefix("3d06:bad:b01:4::/64")) {
		t.Fatal("unknown peer must be rejected")
	}
}
```

Add an `AuditAdjRibIn` test feeding the fake a path outside the allocation and asserting one `Violation` naming the router.

- [ ] **Step 2: Run and verify failure** (`make -C mwan/go build`).

- [ ] **Step 3: Implement**

`AllowedPath`: containment check, prefix inside any of the peer's router allocations (`alloc.Contains(prefix.Addr()) && prefix.Bits() >= alloc.Bits()` via `Prefix.Overlaps` plus bits comparison). `AuditAdjRibIn`: iterate peers, `ListPath` adj-rib-in, collect violations. The agent runs the audit on a one-minute ticker and raises the notifier alert kind `bgp-undeclared-prefix` with router and prefix fields, resolving when the audit is clean (wire the notifier call in Task 5 where the agent owns a notifier).

- [ ] **Step 4: Verify pass** (`make -C mwan/go build`).

- [ ] **Step 5: Commit**

```bash
git add mwan/go/internal/bgp/guard.go mwan/go/internal/bgp/guard_test.go mwan/go/internal/bgp/speaker.go mwan/go/internal/bgp/speaker_test.go
git commit -S -m "Add per-router allocation guard and adj-rib-in audit" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task 4: Learned-route FIB installer

**Files:**
- Create: `mwan/go/internal/bgp/fib_linux.go`, `mwan/go/internal/bgp/fib_stub.go` (non-linux no-op)
- Modify: `mwan/go/internal/netif/state.go` (add `Protocol int` to `RouteSpec`; set it in `buildTableRoute` on the `netlink.Route`; add a new `DeleteTableRoute(ctx, log, want RouteSpec) error` plus its `delTableRouteNetlink` helper, because only the default-route delete exists today and `RouteDel` on a prefix needs `Dst` and `Table` set, swallowing ENOENT and ESRCH the way the default deleter does), `mwan/go/internal/netif/inspect.go` (add `ListProtocolRoutes(ctx, log, family, tableID, protocol int)` beside `ListDHCPRoutes`, using `RT_FILTER_TABLE|RT_FILTER_PROTOCOL`)
- Test: `mwan/go/internal/bgp/fib_linux_test.go`

**Interfaces:**
- Produces: `bgp.FIB` with `New(fibCfg FIBConfig, log *slog.Logger) *FIB`, `FIBConfig{Tables []int, InternalIface string, Shadow bool}`, methods `Apply(ctx, ev PathEvent) error` and `SweepStale(ctx) error`; `PathEvent{Peer string, Prefix netip.Prefix, NextHop netip.Addr, Withdrawn bool}`.
- Consumes: `netif.ReconcileTableRoute` and route deletion with `Protocol: unix.RTPROT_BGP`; Task 3's `AllowedPath`. Route writes go through an injectable `routeWriter` interface (the wanroutes `resolveNextHop` pattern) so tests never touch netlink.

Semantics (from spec contract 3):

- On a best-path add: install `{Family: inet6, Dest: prefix, Via: nextHop, Dev: InternalIface, TableID: t, Protocol: 186}` for every `t` in `Tables`.
- On withdraw or session-down for a peer: delete exactly the routes inside that peer's allocations.
- Never delete on `Stop`; on start, `SweepStale` removes proto-186 routes in owned tables that are absent from the current desired set, and it runs only after every configured peer has either established or timed out once (a `sync.Once` armed by the peer watcher).
- `Shadow: true` logs each intended install and delete at INFO with `shadow=true` and mutates nothing.

- [ ] **Step 1: Write the failing tests** with a recorded `routeWriter` fake: install fan-out across `Tables`, withdraw removes only the withdrawn prefix, shadow records zero writes, sweep removes a stale route and spares desired ones.

- [ ] **Step 2: Run and verify failure** (`make -C mwan/go build`).

- [ ] **Step 3: Implement** `fib_linux.go` plus the two netif extensions. Wire the speaker: register a `WatchEvent` table/best-path callback that converts GoBGP path updates into `PathEvent` (guard through `AllowedPath`; a rejected event increments the audit path, never reaches the FIB) and a peer-down hook that synthesizes withdraws for that peer's allocations.

- [ ] **Step 4: Verify pass** (`make -C mwan/go build`).

- [ ] **Step 5: Commit**

```bash
git add mwan/go/internal/bgp/ mwan/go/internal/netif/
git commit -S -m "Install BGP-learned router prefixes into the kernel tables with shadow and sweep semantics" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task 5: Agent wiring

**Files:**
- Modify: `mwan/go/internal/agent/main.go`
- Test: extend `mwan/go/internal/bgp/speaker_test.go` for the table-set derivation helper

**Interfaces:**
- Consumes: Tasks 2 to 4. The table set comes from the loaded config: `unix.RT_TABLE_MAIN` plus each `[ifmgr.wan.<name>]` table id when that section exists (the failover LXC has none, so its set is main only).
- Produces: the agent constructs `bgp.FIB` when `cfg.BGP.Routers` is non-empty, passes `Shadow: cfg.BGP.RoutesShadowMode`, starts the one-minute adj-rib-in audit ticker, and routes `bgp-undeclared-prefix` through the existing `notifier`.

- [ ] **Step 1: Write the failing test** for `TablesFromConfig(cfg *config.Config) []int` (main only without WANs; main plus WAN tables with them).
- [ ] **Step 2: Run and verify failure** (`make -C mwan/go build`).
- [ ] **Step 3: Implement** the helper and the `agent/main.go` wiring, including the GR shutdown path: `Stop` never calls into the FIB.
- [ ] **Step 4: Verify pass** (`make -C mwan/go build`).
- [ ] **Step 5: Commit**

```bash
git add mwan/go/internal/agent/main.go mwan/go/internal/bgp/
git commit -S -m "Wire the learned-route FIB and adj-rib-in audit into the agent" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task 6: wan.routes static-route shadow gate

**Files:**
- Modify: `mwan/go/internal/ifmgr/modules/wanroutes/wanroutes.go` (`Config`, `appendWANInternalRoutes`, `appendMainInternalRoute`), the wan.routes config mapping in `mwan/go/internal/config/ifmgr_modules.go` and its cmd-side builder (`buildWANRoutesConfig`)
- Test: `mwan/go/internal/ifmgr/modules/wanroutes/wanroutes_test.go`

**Interfaces:**
- Produces: `wanroutes.Config.BGPRoutesShadowMode bool`, rendered from group var `mwan_bgp_routes_shadow_mode` (the same var Task 7 feeds `[bgp] routes_shadow_mode`, so one flag flips both sides).
- Behavior: shadow true keeps today's exact route set. Shadow false omits only the `InternalPrefix` via-edge route from `appendWANInternalRoutes` and `appendMainInternalRoute`; the transit v4 net route and the edge `/128` on-link route stay in every case, because the learned routes still need the edge next hop resolvable.

- [ ] **Step 1: Write the failing test**: with `BGPRoutesShadowMode: false`, `desiredState` returns no route whose `Dest == cfg.InternalPrefix`; with true, the returned set equals today's `routesForGateways` expectation unchanged.
- [ ] **Step 2: Run and verify failure** (`make -C mwan/go build`).
- [ ] **Step 3: Implement** the two-line gates plus the config threading.
- [ ] **Step 4: Verify pass** (`make -C mwan/go build`).
- [ ] **Step 5: Commit**

```bash
git add mwan/go/internal/ifmgr/modules/wanroutes/ mwan/go/internal/config/ mwan/go/cmd/mwan/
git commit -S -m "Gate the static internal-prefix route behind the BGP routes shadow flag" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task 7: Group vars and template rendering

**Files:**
- Modify: `mwan/config/config-vm.toml.j2` (the `[bgp]` block near line 90)
- Modify: `ansible/inventory/group_vars/mwan_servers.yml`, `mwan_suburban_servers.yml`, `mwan_failover_servers.yml`, `mwan_failover_suburban_servers.yml`
- Modify: `ansible/inventory/group_vars/all/service_mapping.yml` only if the router transit addresses are not already entries

**Interfaces:**
- Produces: `mwan_bgp_routers`, a list of `{name, address_v4, address_v6, allocations_v6}` read from `service_mapping` references, and `mwan_bgp_routes_shadow_mode: true` in all four MWAN group var files. Every address and allocation comes from an existing service inventory entry; this task adds no new address to the inventory. The template renders:

```jinja
routes_shadow_mode = {{ mwan_bgp_routes_shadow_mode | ternary('true', 'false') }}

{% for router in mwan_bgp_routers %}
[[bgp.routers]]
name = "{{ router.name }}"
address_v4 = "{{ router.address_v4 }}"
address_v6 = "{{ router.address_v6 }}"
allocations_v6 = {{ router.allocations_v6 | to_json }}

{% endfor %}
```

- The legacy `[[bgp.neighbors]]` / `[[bgp.neighbors_v6]]` loops render only while `mwan_bgp_routers` is empty on a host, expressed with an explicit flag variable, never `is defined`.
- Day-one values: one router entry per environment, the OPNsense router, whose allocations are the specific `/64`s that router actually serves inside the internal block, read from the inventory and the router's own interface set. Not the whole block: spec contract 1 requires allocations disjoint across routers, so a whole-block entry would reject every later router by construction.

- [ ] **Step 1: Edit the group vars and template.**
- [ ] **Step 2: Verify**: `go run goodkind.io/configs/cmd/configs lint` exits 0; a `--check --diff` render on the testbed limit shows the expected `[bgp]` block.
- [ ] **Step 3: Commit**

```bash
git add mwan/config/config-vm.toml.j2 ansible/inventory/group_vars/
git commit -S -m "Render per-router BGP entries and the routes shadow flag into the MWAN config" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task 8: Testbed router-2 simulator

**Files:**
- Modify: `ansible/inventory/group_vars/all/service_mapping.yml` (new `router2_suburban` entry: vmid in the testbed 9xx range, transit v4/v6 addresses, allocation)
- Modify: `opentofu/suburban/` container set (one Debian LXC on the transit bridge, `prevent_destroy`, `local-zfs`)
- Create: `ansible/playbooks/deploy-router2-sim.yml` (installs `frr`, enables `bgpd`, templates `/etc/frr/frr.conf`)
- Create: `ansible/playbooks/templates/router2-frr.conf.j2`

**Interfaces:**
- The FRR config peers with both speaker transit addresses in AS 4200000001, announces the allocation from `service_mapping`, and binds a loopback address inside the allocation so AC2 has a pingable host:

```jinja
router bgp 4200000001
 bgp router-id {{ service_mapping.router2_suburban.ipv4_transit }}
 neighbor {{ service_mapping.mwan_suburban.ipv6_transit }} remote-as 4200000001
 neighbor {{ service_mapping.mwan_failover_suburban.ipv6_transit }} remote-as 4200000001
 address-family ipv6 unicast
  network {{ service_mapping.router2_suburban.allocation_v6 }}
  neighbor {{ service_mapping.mwan_suburban.ipv6_transit }} activate
  neighbor {{ service_mapping.mwan_failover_suburban.ipv6_transit }} activate
 exit-address-family
```

- [ ] **Step 1: Add the service_mapping entry, the OpenTofu resource, and the playbook plus template.**
- [ ] **Step 2: Verify**: `configs lint` green; `configs tofu plan` shows exactly the one new LXC; deploy with `go run goodkind.io/configs/cmd/configs deploy router2-sim --limit router2_suburban_servers` after tofu apply (both are testbed-only commands).
- [ ] **Step 3: Commit**

```bash
git add ansible/ opentofu/suburban/
git commit -S -m "Add the testbed router-2 FRR simulator guest and deploy playbook" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task 9: Testbed validation matrix (operator-gated; every run is a deploy)

No code. Execute the spec's acceptance criteria on the testbed, one cycle per change, resetting per AC8. Record command output per criterion.

- [ ] AC7 first: deploy with both shadow flags on, compare the agent's shadow install log against the live static route set.
- [ ] AC1/AC2: flip testbed `mwan_bgp_routes_shadow_mode` to false (operator go), deploy, onboard router-2 by config alone, prove forward and return traffic from a source inside its allocation (`ping6 -I <allocation host>` from the sim toward a WAN target, plus the reverse).
- [ ] AC3: add an out-of-allocation `network` statement on the sim, confirm no route installs and the `bgp-undeclared-prefix` alert fires, then remove it.
- [ ] AC4: `systemctl stop frr` on the sim; routes gone within the hold timer; OPNsense-testbed routes untouched.
- [ ] AC5: force failover (`mwan watchdog failover` path on suburban); router-2 return traffic flows via the failover LXC.
- [ ] AC6: `systemctl restart mwan-agent` on the testbed VM with GR on; learned routes persist throughout (watch `ip -6 route show proto bgp table all` in a loop).
- [ ] AC8: every OPNsense config.xml edit in these cycles follows the every-change gate (snapshot without RAM, change, traffic proof, reset).
- [ ] AC9: the serial-only revert drill of the FRR announcement change on the testbed OPNsense.
- [ ] Production ships only after all of the above, with explicit per-flip approval; the prod OPNsense FRR network statements are a manual GUI exercise per the spec.
