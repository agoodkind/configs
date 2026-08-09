# Multiple downstream routers over BGP: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** MWAN learns each downstream router's LAN prefixes over iBGP and installs them as kernel return routes, so adding router N+1 requires nothing on MWAN at all.

**Architecture:** The speaker accepts dynamic BGP sessions from any address on the internal transit segment and installs whatever IPv6 best paths peers announce, tagged proto bgp, into the main table and every WAN table. Neighbors are trusted the way eBGP peers are; there is no import filter and no per-router config. The wan.routes static internal route retires behind a shadow flag. The spec is [spec.md](spec.md).

**Tech Stack:** Go (gobgp v4, vishvananda/netlink via internal/netif), Ansible group vars and Jinja templates, FRR on the testbed router-2 simulator.

## Global Constraints

- Every Go slice must pass the full acceptance gate end to end before it commits: `make -C mwan/go build`. That target runs `proto`, then `build-linux` and `build-mwan-opnsense`, each of which depends on `check` (build, vet, lint, test, govulncheck, staticcheck-extra). Never run raw `go`.
- A slice must contain both a new primitive and its consumer. The deadcode gate rejects an unreachable function, and `exhaustruct` forces every literal of a struct to name a newly added field.
- Single TOML config: every new knob lives in `/etc/mwan/config.toml`, rendered from group vars. No env-var config.
- No `| default()` or `is defined` on Ansible input variables; `configs lint` enforces.
- iBGP AS is `4200000001`. Learned kernel routes carry protocol `unix.RTPROT_BGP` (186).
- Shadow-first: the shadow flag defaults to shadow on every host; authoritative flips are separate, operator-approved deploys, testbed before production.
- Files stay under 500 lines; split by responsibility.
- Commits are signed (`git commit -S`) with the `Co-authored-by: Claude <noreply@anthropic.com>` trailer.

The config schema, speaker, learned-route installer, agent wiring, and wan.routes shadow gate are on trunk. They carry a per-router trust surface (declared routers with allocation validation, a per-peer import guard, and an undeclared-prefix audit) that the trust-the-neighbor decision removes.

---

### Task A: Trust announcements and accept dynamic peers

**Files:**
- Modify: `mwan/go/internal/config/config.go` (BGPSection), `mwan/go/internal/bgp/config.go`, `mwan/go/internal/bgp/speaker.go`, `mwan/go/internal/bgp/fib_linux.go`, `mwan/go/internal/agent/bgp.go`, `mwan/go/internal/agent/bgp_linux.go`, `mwan/go/internal/agent/bgp_stub.go`
- Delete: `mwan/go/internal/bgp/guard.go`, `mwan/go/internal/bgp/guard_test.go`, `mwan/go/internal/config/bgp_routers_test.go`
- Test: `mwan/go/internal/bgp/speaker_test.go`, `mwan/go/internal/bgp/fib_linux_test.go`, config package tests

**Interfaces:**
- Produces: `BGPSection.DynamicNeighbors []string` (`toml:"dynamic_neighbors"`, CIDR prefixes the speaker accepts sessions from), threaded to `bgp.Config.DynamicNeighborPrefixes []netip.Prefix` by the agent with parse errors fatal at startup.
- Removes: `config.BGPRouter`, `BGPSection.Routers`, `validateBGPRouters`, `bgp.Router`, `Config.Routers`, `routerForPeer`, `AllowedPath`, `AuditAdjRibIn`, `Violation`, and the agent's audit ticker plus the `bgp-undeclared-prefix` alert.
- Keeps: `BGPSection.RoutesShadowMode`, the FIB (`NewFIB`, `Apply`, `SweepStale`, per-peer desired tracking), graceful restart, `TablesFromConfig`.

Semantics:

- Speaker start registers a passive peer group in AS 4200000001 and adds each configured dynamic-neighbor prefix through the GoBGP dynamic-neighbor API, alongside the existing static neighbor loops. Read the gobgp v4.7.0 module cache for the exact `AddPeerGroup` and `AddDynamicNeighbor` shapes; do not guess them.
- Validation requires at least one of: static v4 neighbors, static v6 neighbors, or dynamic-neighbor prefixes.
- The best-path watch installs only IPv6 unicast paths learned from a remote peer. Locally originated paths, the announced defaults, never reach the FIB.
- `SweepStale` arms after a startup grace period of twice the hold time, replacing the all-configured-peers-settled arming, because dynamic peers have no configured set. Document the constraint at the arming site.
- Peer-down withdraws by the FIB's own per-peer tracking, which already maps each installed prefix to the peer that announced it.

- [ ] **Step 1: Write the failing tests** (dynamic prefix validation; speaker registers the peer group plus one dynamic neighbor per prefix on the fake; the watch installs a remote-sourced path and skips a locally sourced one; the sweep arms only after the grace period).
- [ ] **Step 2: Run and verify failure** (`make -C mwan/go build`).
- [ ] **Step 3: Implement**, deleting the guard and audit surface in the same slice.
- [ ] **Step 4: Verify pass** (`make -C mwan/go build`).
- [ ] **Step 5: Commit**

```bash
git add mwan/go/
git commit -S -m "Accept dynamic BGP peers on the transit link and drop the per-router import guard" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task B: Render the dynamic-neighbor prefixes and shadow flag

**Files:**
- Modify: `mwan/config/config-vm.toml.j2` (the `[bgp]` block and `[ifmgr.modules.wan.routes]`), `mwan-failover/config.toml.j2` (the `[bgp]` block)
- Modify: `ansible/inventory/group_vars/mwan_servers.yml`, `mwan_suburban_servers.yml`, `mwan_failover_servers.yml`, `mwan_failover_suburban_servers.yml`

**Interfaces:**
- Produces: `mwan_bgp_dynamic_neighbors` (list of transit CIDRs) and `mwan_bgp_routes_shadow_mode: true` in all four MWAN group var files. Templates render `dynamic_neighbors` and `routes_shadow_mode` under `[bgp]`, and the VM template renders `bgp_routes_shadow_mode` under `[ifmgr.modules.wan.routes]`.
- Values: production `10.250.250.0/29` and `3d06:bad:b01:fe::/64`; testbed the transit net and transit `/64` already carried by the group vars and service inventory. No new address enters the inventory.

- [ ] **Step 1: Edit the group vars and both templates.**
- [ ] **Step 2: Verify**: `go run goodkind.io/configs/cmd/configs lint` exits 0; a `--check --diff` render on the testbed limit shows the expected `[bgp]` block.
- [ ] **Step 3: Commit**

```bash
git add mwan/config/config-vm.toml.j2 mwan-failover/config.toml.j2 ansible/inventory/group_vars/
git commit -S -m "Render dynamic BGP neighbor prefixes and the routes shadow flag" -m "Co-authored-by: Claude <noreply@anthropic.com>"
```

---

### Task C: Testbed router-2 simulator

**Files:**
- Modify: `ansible/inventory/group_vars/all/service_mapping.yml` (new `router2_suburban` entry: vmid in the testbed 9xx range, transit addresses, and the prefix it announces)
- Modify: `opentofu/suburban/` container set (one Debian LXC on the transit bridge, `prevent_destroy`, `local-zfs`)
- Create: `ansible/playbooks/deploy-router2-sim.yml` (installs `frr`, enables `bgpd`, templates `/etc/frr/frr.conf`, persists a loopback address inside the announced prefix)
- Create: `ansible/playbooks/templates/router2-frr.conf.j2`

The FRR config peers with both speaker transit addresses in AS 4200000001 and announces the simulator's prefix. The prefix is carved at this step against the live testbed: an unused `/64` under the testbed guest space, recorded only in the service inventory.

- [ ] **Step 1: Add the service_mapping entry, the OpenTofu resource, and the playbook plus template.**
- [ ] **Step 2: Verify**: `configs lint` green; `configs tofu plan` shows exactly the one new LXC; deploy is operator-gated.
- [ ] **Step 3: Commit**

---

### Task D: Testbed validation matrix (operator-gated; every run is a deploy)

No code. Execute the spec's acceptance criteria on the testbed, one cycle per change, resetting per AC8. Record command output per criterion.

- [ ] AC7 first: deploy with the shadow flag on, compare the agent's shadow install log against the live static route set.
- [ ] AC1/AC2: flip testbed `mwan_bgp_routes_shadow_mode` to false (operator go), deploy, bring up router-2 with no MWAN change, prove forward and return traffic from a source inside its prefix.
- [ ] AC3: add a `network` statement on the sim and watch the route install; remove it and watch the route withdraw, other routes untouched.
- [ ] AC4: `systemctl stop frr` on the sim; routes gone within the hold timer; OPNsense-testbed routes untouched.
- [ ] AC5: force failover; router-2 return traffic flows via the failover LXC.
- [ ] AC6: `systemctl restart mwan-agent` on the testbed VM with GR on; learned routes persist throughout (watch `ip -6 route show proto bgp table all` in a loop).
- [ ] AC8: every OPNsense config.xml edit in these cycles follows the every-change gate (snapshot without RAM, change, traffic proof, reset).
- [ ] AC9: the serial-only revert drill of the FRR announcement change on the testbed OPNsense.
- [ ] Production ships only after all of the above, with explicit per-flip approval; the prod OPNsense FRR network statements are a manual GUI exercise per the spec.
