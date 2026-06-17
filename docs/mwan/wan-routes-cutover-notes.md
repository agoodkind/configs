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

## Open items before the prod-mirror baseline is complete

- Run the deploy chain so VM 950 runs current binaries/config like prod
  (`deploy-mwan --limit test_mwan_servers`, then failover/opnsense/testbed as
  needed). VM 950 was last on an old binary.
- `mwan-watchdog-testbed` is stopped (to end the snapshot storm) and its host
  config now targets `204::950`; redeploy its config and fix the storm cause
  before restarting.
- Snapshot the clean prod mirror as the cutover reset point.

## Cutover sequence (after the baseline snapshot)

Per the plan: shadow, then dual-write, then remove the dispatcher hook, then the
health-daemon call, then the boot oneshot, then delete the shell. Validate the
late-RA convergence fix at the shadow step. Any failure resets to the snapshot.
