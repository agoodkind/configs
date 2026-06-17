# wan_routes cutover test notes

Working notes for bringing the suburban testbed to prod parity and then testing
the `wan_routes` ifmgr cutover on it. The discipline: establish a faithful
pre-cutover prod mirror first, snapshot it as the reset point, then run the
cutover sequence; any failure resets to the prod-mirror snapshot and restarts.

## Goal and rules

- Mirror prod as it is now (pre-cutover), then test the cutover.
- Establish prod parity first, snapshot it, then begin cutover proceedings.
- Every failure resets to the prod-mirror snapshot and restarts from the top.
- Mimic prod closely as its current state.

## Prod reference (current, pre-cutover)

- mwan VM 113 `enmgmt0` is on the OPNsense LAN `/64` (`3d06:bad:b01::/64`,
  `::113`), a 12-line `10-mgmt.network` with no policy route, DNS at OPNsense
  `::1`. It is reachable on-link via the OPNsense-routed LAN; its main default
  route is a WAN, so off-segment replies do not return.
- Convergence is the shell `update-routes.sh` (dispatcher hook + boot oneshot +
  health daemon). `wan_routes` is not deployed on prod (`mwan_ifmgr_wan_enabled`
  is false on `mwan_servers`).

## Prod edge cases found while mirroring (durable)

These are latent prod-relevant issues surfaced by the testbed work.

1. **`kvm_arguments` drift in tofu.** The suburban VM 950 resource omits
   `kvm_arguments` (Ansible owns the live `args` vhost-vsock device because the
   Proxmox API rejects token writes to `args`), but tofu state captured it, so a
   plan wanted to null it and the apply failed with `VM is locked`. Fix:
   `lifecycle.ignore_changes = [kvm_arguments]`. The same pattern applies to any
   VM whose `args` is Ansible-owned (VM 101, and prod VMs if imported similarly).
2. **Cloud-init drive storage.** VM 950 declared `initialization.datastore_id =
   "local-lvm"`, but `local-lvm` is disabled on suburban (only `local-zfs` is
   active). A cloud-init drive regen failed with `storage 'local-lvm' is not
   available`. Fix: point it at the active pool (`local-zfs`).
3. **Management symmetric-return routing.** A dedicated policy table that carries
   only a default route shadows the on-link connected route, so replies to
   on-link peers triangle through the gateway and are lost. Prod avoids this by
   having no policy route (its mgmt `/64` is on-link to its clients). Mirror that:
   no mgmt policy route; reach an off-segment management host on-link via a jump.
4. **Watchdog snapshot storm.** `mwan-watchdog-testbed` retried `qmsnapshot` on
   VM 950 about every 33s, each failing `VM is locked (snapshot)` from one wedged
   snapshot lock, holding the lock indefinitely. A stuck snapshot plus a tight
   retry loop is a denial-of-service on the VM lock. Recovery: stop the watchdog,
   `qm unlock 950`. Open: the retry cadence and the wedged-snapshot handling are
   a watchdog bug to fix before re-enabling.
5. **ICMP vs TCP reachability.** The testbed OPNsense blocks ICMP echo to LAN
   hosts but allows TCP. Measure reachability with the protocol that matters
   (TCP/SSH), not `ping6`, or a healthy host reads as down.

## Prod-mirror state established (2026-06-16)

- VM 950 management re-segmented onto the `vmbrtrunk` `204::` services LAN
  (`204::950`), beside the OPNsense MANAGEMENT interface (`204::1`) and the DNS64
  LXC (`204::464`), mirroring prod's mwan-on-the-OPNsense-LAN topology. tofu
  `opentofu/suburban/vms.tf`.
- 12-line `10-mgmt.network` (no policy route), DNS at `204::1`. Reachable on-link
  from the suburban host (`204::5`) and from the controller via a ProxyJump
  through the host, mirroring prod's on-link access.
- `mwan_ifmgr_wan_enabled: false` on the testbed baseline (shell convergence,
  like prod).

## Prod-mirror baseline VERIFIED and snapshotted (2026-06-17)

`deploy-mwan --limit test_mwan_servers` ran green (ok=148, failed=0; the lone
`unreachable` is the post-reboot disconnect). The deploy itself rewrote the
durable `10-mgmt.network`, replacing the one-time QGA bootstrap. Verified on
VM 950 after reboot:

- Services active: `mwan-agent`, `mwan-health`, `systemd-networkd`,
  `networkd-dispatcher`, `nftables`. `wan_routes` (`mwan-ifmgr@wan`) is
  not-found/inactive (baseline off, like prod).
- BGP established to the testbed OPNsense (`201::2`, `10.250.250.2`), defaults
  `0.0.0.0/0` and `::/0` announced.
- Shell `update-routes.sh` converged: v4+v6 fwmark rules (100/200/300) and v6
  from-PD rules (55/56/57), per-WAN tables with the internal `210::/60` and the
  webpass default. Health `att:healthy webpass:healthy`.
- DNS resolves via `204::1`. Reachable via the suburban ProxyJump.

Snapshot `prod-mirror-pre-cutover` (VM 950, disk-only, no saved RAM) is the
cutover reset point.

### Deploy-path fixes made to reach a green testbed deploy (prod-safe)

The deploy-mwan -> testbed path was new (Phase 3) and never run; each gap was
fixed by parameterizing per environment, not patching:

- `opnsense_addr` = `204::1` (on-link OPNsense for VM 950).
- OPNsense BGP cluster (`opnsense_gateway_names`, `opnsense_bgp_*`) declared for
  the testbed.
- `discover-runtime-network.yml` delegates to `mwan_proxmox_delegate` (inventory
  host), not the PVE node name.
- AT&T 802.1X/ONT/VLAN tasks extracted to `tasks/mwan-vm/att-8021x.yml`, gated on
  `mwan_att_8021x_enabled` (true prod, false testbed).
- `mwan_networkd_files` per environment (testbed uses the direct-link att/webpass
  under `mwan/networkd/testbed/`, no VLAN).
- `mwan_enabled_services` per environment (testbed omits the 802.1X units).

### Watchdog: intentionally stopped during the cutover test

`mwan-watchdog-testbed` stays stopped through the controlled cutover test so its
auto-rollback cannot fight the manual reset-to-snapshot discipline, and to avoid
the snapshot-storm bug. Its host config now targets `204::950` (committed,
`deploy-testbed` pending). Restore it (config redeploy + storm-cause fix) after
the cutover test concludes.

## Cutover sequence (after the baseline snapshot)

Per the plan: shadow, then dual-write, then remove the dispatcher hook, then the
health-daemon call, then the boot oneshot, then delete the shell. Validate the
late-RA convergence fix at the shadow step. Any failure resets to the snapshot.
