# Tickets to file (Tack MCP not connected this session)

Filed during cleanup pass on 2026-04-29 with no MCP write path. Paste each
into Tack manually or on the next session when MCP comes back online.

## 1. Cleanup pre-cutover keepalived files + apt package

**Project**: MWAN
**Priority**: low
**State**: open

**Description**:

Service is inactive + disabled on both VM 113 and LXC 116 (already done in
the keepalived stop+disable ticket). But the following stragglers remain.

VM 113 stragglers:
- `/etc/keepalived/check_internet.sh`
- `/etc/keepalived/keepalived.conf`
- `/etc/keepalived/keepalived.conf.sample`
- `/etc/keepalived/keepalived.config-opts`
- `/etc/keepalived/notify.sh`
- `keepalived` apt package still installed

LXC 116: same set of files + package.

Removal procedure:
1. `apt -y remove keepalived` on each host
2. `rm -rf /etc/keepalived/` on each host
3. Hunt down any Ansible role / Jinja templates in configs repo that
   maintain these. Likely candidates: `ansible/roles/keepalived/`,
   `mwan/config/keepalived*.j2`, possibly tasks in `deploy-mwan.yml`.
4. Delete those Ansible bits.
5. Run a fresh playbook deploy to confirm idempotence with the new
   smaller surface.

No functional impact. Cleanup only. Reduces template surface for future
ifmgr work and shrinks the mental footprint of the prod stack.

## 2. Delete stale prod PVE snapshots

**Project**: MWAN
**Priority**: low
**State**: open

**Description**:

13 stale snapshots reclaimable on prod (vault) local-zfs / local-lvm:

VM 113 (delete):
- `pre-iifname-20260113-135743` (Jan 13, pre-iifname-change escape hatch, 3.5 mo)
- `pre-deploy-20260122T074211` (Jan 22, watchdog auto-rollback artifact)
- `manual-good-state-20260122-091131` (Jan 22, manual baseline)
- `pre-deploy-20260122T101859` (Jan 22, watchdog auto-rollback artifact)
- `pre-deploy-20260122T124746` (Jan 22, watchdog auto-rollback artifact)
- `post-bgp-cutover-20260427-233904` (Apr 27, first post-cutover known-good,
  superseded by 3 newer known-good snapshots)

VM 113 (keep): 3 most recent `known-good-20260501-*` snapshots. Watchdog
rotation; do not touch.

OPNsense VM 101 (delete):
- 7x `scheduled-2025111*` (Nov 10-16 2025, 5.5 months old, scheduled snap
  that stopped rotating)
- `pre-bgp-cutover-2026-04-26` (Apr 25, cutover escape hatch, cutover
  succeeded 4 days ago, prod healthy)
- `pre-decommission-prod-20260427` (Apr 27, decommission escape hatch,
  decom succeeded 2 days ago)

Procedure: `qm delsnapshot <vmid> <name>` for VMs, `pct delsnapshot
<vmid> <name>` for LXCs. Run from vault (`ssh root@3d06:bad:b01:ff::1`).

After deletion, verify watchdog is still happy and pointing at one of the
remaining `known-good-20260501-*` snapshots:
- `cat /run/mwan-rollback.state` on vault
- `journalctl -u mwan-watchdog -n 20`

## 3. EPIC: rewrite remaining mwan shell + systemd-networkd state into Go monolith

**Project**: MWAN
**Priority**: medium
**State**: open
**Type**: epic

**Description**:

Following the ifmgr migration (which moved address/route/rule/RA/DHCP/probe
state into the Go monolith), the remaining shell scripts and
systemd-networkd configuration are the next layer of stuff to absorb.

### Inventory of shell scripts to migrate (in mwan/scripts/ + mwan/hooks/)

- `mwan-update-npt-all.sh` (NPT prefix translation programming)
- `mwan-debug.sh` (debug snapshot bundle generator)
- `mwan-trace-boot.sh` (boot-time trace logger)
- `mwan-wait-routes-prereqs.sh` (boot blocker until routes installed)
- `mwan-wait-npt-prereqs.sh` (boot blocker until NPT inputs ready)
- `update-npt.sh` (per-iface NPT update)
- `update-routes.sh` (route programming)
- `health-check.sh` (multi-target connectivity probe)
- `find-pd-prefixes.sh` (DHCPv6 PD prefix discovery)
- `bringup-att-vlan.sh` (AT&T 802.1X + VLAN setup)
- `wpa-action.sh` (wpa_supplicant event handler)
- `wpa-wait-att-iface.sh` (wpa_supplicant boot wait)
- `update-att-pinned-dests.sh` (nft set maintenance for AT&T pinning)
- `opnsense-remove-gatewayv6.sh` (cutover-era helper, possibly already obsolete)
- `opnsense-restore-gatewayv6.sh` (cutover-era helper, possibly already obsolete)
- `mwan/hooks/50-update-routes.sh` (networkd-dispatcher hook)
- `mwan/hooks/55-update-npt.sh` (networkd-dispatcher hook)

### Inventory of systemd-networkd state to absorb

- `/etc/systemd/network/*.network` files for AT&T, Webpass, MB, internal,
  management ifaces
- DHCPv6 PD client behavior on AT&T + Webpass + MB
- 802.1X (EAP) coordination via wpa_supplicant on AT&T
- VLAN tagging (3242 on AT&T, etc.)
- Bridge membership for OPNsense-side internal bridge

### Suggested structure for the new ifmgr modules

- `npt`: NPT prefix translation programming via netlink (replaces
  update-npt.sh + mwan-update-npt-all.sh)
- `pd_client`: DHCPv6-PD client per WAN iface (replaces find-pd-prefixes.sh
  + the systemd-networkd DHCPv6 client config)
- `wpa_802_1x`: wpa_supplicant lifecycle per WAN iface (replaces
  bringup-att-vlan.sh + wpa-action.sh + wpa-wait-att-iface.sh)
- `vlan`: VLAN ifupdown + cleanup (replaces parts of bringup-att-vlan.sh)
- `dhcp_v6_iface`: DHCPv6 client lifecycle for a managed iface
- `nft_pinned_dests`: nft set maintenance for fwmark pinning (replaces
  update-att-pinned-dests.sh)
- `health_probe`: multi-target connectivity probe with thresholds
  (replaces health-check.sh)

### Questions to resolve before implementation

1. Where do these run? VM 113 (mwan router) needs the WAN-side modules.
   LXC 116 (failover backup) needs a smaller subset. Suburban (testbed)
   needs subset for test ifaces.
2. Does this absorb networkd entirely, or do networkd .network files
   continue to manage L2 (bridge membership, VLAN parent device) while
   ifmgr owns L3 (addresses, routes, rules, RA, DHCP, NPT)?
3. New ifmgr roles needed: `mwan-router-primary` (VM 113 full WAN stack),
   `mwan-router-backup` (LXC 116, BGP-only) -- do these supersede the
   existing role names?
4. Migration order: probably NPT first (simplest, well-bounded, replaces
   the most-touched shell script), then nft_pinned_dests, then DHCPv6-PD,
   then wpa_802_1x last (most complex due to event-driven 802.1X timing).

### Acceptance criteria

- All scripts in `mwan/scripts/` + `mwan/hooks/` removed
- `/etc/systemd/network/*.network` files reduced to L2-only or removed
- `mwan/Makefile` no longer needs to deploy shell scripts at all
- VM 113 + LXC 116 + suburban testbed boot cleanly with new ifmgr modules
  active
- Cutover2 E2E still passes on testbed

### Sub-tickets to file as work begins

(file as separate stories per module above)
