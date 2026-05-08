# Plan: Bring suburban testbed network infrastructure to prod parity

Tracking ticket: `MWAN-140`. Parent arc: `MWAN-13` (OPNsense 26.x upgrade).

Two design decisions in this plan come from `MWAN-148`. The first decision moves device-name remapping out of FreeBSD `rc.conf` and into the `config.xml` transform layer described in slice 4. The second decision drops the separate testbed MANAGEMENT bridge so that the testbed mirrors prod's one-port posture (MANAGEMENT untagged plus the VLAN trunk on the same device). Both decisions are noted inline where they affect the plan.

## Context

The 26.x upgrade strategy is testbed-first: bring testbed to the closest possible match of prod, run the upgrade there, capture issues, mediate them, then upgrade prod. Two parity axes were already named: email behavior (`MWAN-132`, in flight) and OPNsense `config.xml` shape (`MWAN-117/118/119/127`). This plan covers a third axis that the config-import work has been silently hitting: testbed **infrastructure** does not match prod's interface shape, so any prod-shaped `config.xml` applied on testbed targets interface names and parents that do not exist. `MWAN-119 v1` and `v2` both failed for related reasons, then rolled back.

## Drift summary (live state captured 2026-05-07)

### Prod OPNsense (vault VM 101, OPNsense 25.7)

Physical and virtual interfaces from `ifconfig` and `config.xml`:

| Interface | Kind | Description | IPv4 | IPv6 | Parent |
| --- | --- | --- | --- | --- | --- |
| `vtnet1` | virtio | WAN | 10.250.250.2/29 | 3d06:bad:b01:fe::2/64 | net3 to mwanbr |
| `vtnet0` | virtio | VMNET (opt6) | 10.250.0.1/24 | 3d06:bad:b01::1/64 | net0 to vmbr0 |
| `iavf0` | PCI VF | MANAGEMENT (opt9) and VLAN trunk parent | 10.250.4.1/24 | 3d06:bad:b01:4::1/64 | hostpci0 02:0a |
| `vlan0100` | 802.1q | PRIVILEGED (lan) | 10.250.1.1/24 | 3d06:bad:b01:1::1/64 | iavf0 |
| `vlan0200` | 802.1q | GENERAL (opt4) | 10.250.2.1/24 | 3d06:bad:b01:2::1/64 | iavf0 |
| `vlan0300` | 802.1q | CAPTIVE (opt5) | 10.250.3.1/24 | none | iavf0 |
| `vlan064` | 802.1q | IPv6OnlyVLAN (opt8) | none | 3d06:bad:b01:64::1/64 | iavf0 |
| `wg0` | WireGuard | WG (opt1) | 10.250.10.1/24 plus alias 10.240.10.2/24 | 3d06:bad:b01:10::1/64 | software |
| `nat64` | tun (Tayga) | NAT64 (opt7) | 10.250.46.1/32 | 3d06:bad:b01:64::ffff:1/128 | software |
| `lo0`, `enc0`, `pfsync0`, `pflog0`, `INTERNAL` group, `wireguard` group, `tayga` group | system | n/a | n/a | n/a | system |

Vault host bridges: `vmbr0` (10.250.0.254/24, 3d06:bad:b01::254/64) and `mwanbr` (manual, no IP, carries the BGP/internal link). Prod VM 101 attaches to `vmbr0` (net0) and `mwanbr` (net3), plus PCI passthrough `02:0a` for `iavf0`.

### Testbed OPNsense (suburban VM 101, OPNsense 25.7, currently wedged)

VM 101 hardware today:

| NIC | Bridge | Notes |
| --- | --- | --- |
| `net0` | `vmbr3` (192.168.1.0/24) | LAN-equivalent, currently unaddressed at OPNsense |
| `net1` | `vmbr2` (10.250.250.0/29 plus 3d06:bad:b01:201::/64) | WAN-equivalent |
| no `net2` | n/a | no third NIC at all |
| no PCI passthrough | n/a | no `iavf0` analog available |

Suburban host bridges: `vmbr0` (10.240.0.148/24), `vmbr1` (10.240.200.1/24, 3d06:bad:b01:200::1/64), `vmbr2` (the WAN analog), `vmbr3` (192.168.1.5/24), `vmbr4..6` (manual, used by ISP simulator LXCs 200/201/202).

### Drift call-outs

1. **No VLAN trunk parent on testbed.** Prod uses `iavf0` as the parent of four VLANs and as MANAGEMENT (untagged). Testbed has no equivalent, so any `config.xml` that references `iavf0` or its VLAN children fails on import.
2. **No MANAGEMENT plane on testbed.** Prod's `MANAGEMENT` interface (`iavf0` native, 10.250.4.0/24) has no testbed counterpart.
3. **VMNET addressing differs.** Prod: 10.250.0.0/24 plus 3d06:bad:b01::/64. Testbed: 192.168.1.0/24 on `vmbr3`. Different family, different prefix.
4. **No WG, no NAT64 on testbed.** Both interfaces are configured by prod's `config.xml` and have no testbed counterpart.
5. **VM 101 NIC count is short by at least one.** Prod uses three logical attach points (`vmbr0`, `mwanbr`, PCI VF). Testbed uses two virtio NICs.
6. **ISP simulator LXCs do not match prod's WAN topology.** Prod's WAN side is the real ISP transit on `mwanbr`. Testbed simulates WAN via vmbr2 and three ISP LXCs on vmbr4/5/6. The current attach is correct in shape, not in addressing.

## Approach

Rebuild testbed infrastructure to mirror prod's interface ORDER and TOPOLOGY, with testbed-specific addresses chosen to avoid prod conflicts. Use a trunked virtio NIC in place of the PCI VF, since suburban has no spare PCI hardware to passthrough. Leave the testbed device names at their natural FreeBSD values (`vtnet0`, `vtnet1`, etc.) and have the slice 4 `config.xml` transform rewrite every prod reference to `iavf0` to whatever the testbed equivalent is at the matching slot. Interface order matters; names need not match.

Rejected approach (forensic note): an earlier iteration of this plan renamed the virtio interface in OPNsense to `iavf0` via FreeBSD `/etc/rc.conf` `ifconfig_<orig>_name="iavf0"`. The branch `mwan-140-opnsense-rcconf` carries that work and stays as a forensic artifact of the rejected direction. `MWAN-148` superseded it for two reasons: testbed device names need not match prod since only ordering matters, and centralizing the name remap in the `config.xml` transform layer keeps the FreeBSD side untouched. Do not delete the `mwan-140-opnsense-rcconf` branch; it documents the rejected path.

The `config.xml` transform in slice 4 carries the concrete name-remap deliverable: rewrite every `<if>iavf0</if>` (and any other prod-side reference to `iavf0`) to the testbed's matching device name at the same interface slot.

### Slice plan

Each slice runs in an isolated worktree off local main, same pattern as MWAN-132. All slices are independent except where noted.

#### Slice 1: suburban hypervisor bridge plumbing

Owner files:
- `ansible/playbooks/configure-suburban-hypervisor.yml` (new or extend existing).
- `mwan/testbed/suburban/etc-network-interfaces.j2` (new template).
- `mwan/docs/testbed-infra-bridge-map.md` (new doc).

What changes on suburban:
- Add `vmbrtrunk` bridge with `bridge-vlan-aware yes`. This becomes the parent of the 802.1q children inside OPNsense and ALSO carries the untagged MANAGEMENT plane, mirroring prod's one-port posture (`iavf0` carries MANAGEMENT untagged plus the VLAN trunk on the same physical port). The earlier draft of this plan reserved a separate `vmbr-mgmt` bridge for MANAGEMENT; `MWAN-148` dropped that split so testbed matches prod.
- Reserve VLAN IDs 100, 200, 300, 64 for PRIVILEGED, GENERAL, CAPTIVE, IPv6Only on the trunk.
- Reserve a non-routable bridge for INTERNAL group equivalent if needed (likely not, since INTERNAL is a virtual group with no physical bind).
- All addressing stays in the 10.240.x.0/24 and 3d06:bad:b01:200..209::/64 testbed ranges to avoid clashing with prod.

No `ansible-playbook` apply in this slice. Verification is `ansible-playbook --check --diff`.

#### Slice 2: VM 101 hardware reconfiguration

Owner files:
- `mwan/testbed/vm-101/qm-config.md` (new doc capturing the target shape).
- `ansible/playbooks/configure-suburban-hypervisor.yml` (extend with `community.general.proxmox_vm` tasks to set net mappings).

What changes on VM 101:
- Keep `net0` on `vmbr3` (LAN reach for SSH from suburban).
- Move `net1` to the new `vmbrtrunk`. This single attach carries both the untagged MANAGEMENT plane and the four 802.1q VLAN children, matching prod's one-port posture for `iavf0`. `MWAN-148` removed the previously planned separate `net2` on `vmbr-mgmt`.
- Optionally add `net2` on `mwanbr-equivalent` (currently `vmbr2`) for the BGP-side link.
- Keep the existing `mwanrpc` virtio-serial chardev for MWN1.

This slice does not touch VM 101 directly. It captures the target Proxmox config in repo and as Ansible tasks. The actual `qm set` runs are part of slice 6 (the wiped-baseline rebuild) so we do not destabilize the currently-wedged VM 101.

#### Slice 3: rejected; folded into Slice 4

`MWAN-148` rejected the FreeBSD `rc.conf` rename approach (originally drafted as "rename the virtio NIC to `iavf0` via `ifconfig_<orig>_name="iavf0"`"). The reasoning: testbed device names need not match prod, only the interface order matters; centralizing the asymmetry in the `config.xml` transform layer keeps the FreeBSD side stock and avoids drift on every prod re-import. The `mwan-140-opnsense-rcconf` branch stays as a forensic artifact of the rejected path; do not delete it. This slice's deliverables fold into Slice 4 below.

#### Slice 4: imported config.xml shaping for testbed (now also handles device-name remap)

Owner files:
- `mwan/scripts/opnsense-config-shape-for-testbed.sh` (new) or a Python helper.
- The redacted candidate already exists at `.claude/worktrees/mwan-redact-opnsense-config/tmp/opnsense-prod-config.redacted.xml` per the handoff.

What this slice produces: a deterministic transform from the prod redacted `config.xml` to a testbed-shaped `config.xml`. The transform substitutes prod IP ranges for testbed equivalents, substitutes the WG peer set with testbed peers, substitutes Tayga prefixes if they collide with prod, AND rewrites every prod-side reference to `iavf0` (e.g. `<if>iavf0</if>`, VLAN `<if>` parents, `<interface>` elements) to the testbed's matching device name at the same interface slot. This name-remap responsibility moved here from the rejected Slice 3 per `MWAN-148`.

The transform output is the input to slice 6.

#### Slice 5: ISP simulator alignment

Owner files:
- `mwan/testbed/lxc-200/`, `lxc-201/`, `lxc-202/` config snapshots.
- `ansible/playbooks/configure-isp-lxcs.yml` (new or extend).

What changes: ensure each ISP simulator LXC presents the right WAN side to OPNsense. Webpass simulator on the bridge that maps to vtnet1 WAN. AT&T simulator on a separate bridge that exercises 802.1X authentication if we want to test that path. Monkeybrains simulator on a third bridge.

Out of scope: actual 802.1X simulation. If that proves difficult, the AT&T simulator skips to plain DHCP and we accept the gap.

#### Slice 6: wiped-baseline rebuild and config import (the actual MWAN-127 execution)

Owner runbooks:
- `mwan/docs/runbooks/opnsense-serial-vm-from-scratch.md` (committed).
- `mwan/docs/runbooks/opnsense-testbed-config-import.md` (committed).

What this slice does: provision a fresh OPNsense VM on suburban using the from-scratch runbook, with the hardware shape from slice 2 (no rc.conf overlay; `MWAN-148` dropped the rename approach). Apply the testbed-shaped `config.xml` from slice 4 via SSH or QGA, observed on the serial console per the import runbook gate. Validate every step. The current wedged VM 101 stays untouched until the new baseline is healthy, then it is decommissioned.

This is the slice that retires `MWAN-127` once it lands clean.

#### Slice 7: documentation

Owner files:
- `AGENTS.md` (new section: "Testbed infrastructure parity").
- `mwan/docs/testbed-prod-parity-matrix.md` (new doc covering all parity axes: email, config.xml, infra).

## Files to change (consolidated)

New:
- `ansible/playbooks/configure-suburban-hypervisor.yml`
- `ansible/playbooks/configure-isp-lxcs.yml`
- `mwan/testbed/suburban/etc-network-interfaces.j2`
- `mwan/testbed/vm-101/qm-config.md`
- `mwan/scripts/opnsense-config-shape-for-testbed.sh`
- `mwan/docs/testbed-infra-bridge-map.md`
- `mwan/docs/testbed-prod-parity-matrix.md`

Note: `mwan/testbed/opnsense/etc-rc.conf-overlay.md` was in an earlier draft of this plan and is no longer a deliverable; `MWAN-148` rejected the rc.conf rename approach.

Modified:
- `AGENTS.md`
- `ansible/inventory/group_vars/mwan_testbed_servers.yml` (extend with the new bridge and VLAN variables)

## Tack tickets

To file via `mcp__tack__tack_create_issue` after this plan is approved. No pre-picked numbers.

Parent: already filed as `MWAN-140`.

Children:
1. Slice 1: suburban hypervisor bridge plumbing (single trunk bridge per `MWAN-148`).
2. Slice 2: VM 101 hardware reconfiguration codified in IaC.
3. Slice 3: REJECTED per `MWAN-148`. Folded into Slice 4. No ticket.
4. Slice 4: testbed-shaped config.xml transform (also performs device-name remap per `MWAN-148`).
5. Slice 5: ISP simulator alignment.
6. Slice 6: wiped-baseline rebuild and config import (closes `MWAN-127` on landing).
7. Slice 7: documentation.

Comments on `MWAN-140` track each slice's commit SHA.

## Verification

Per slice: `ansible-playbook --check --diff` against the appropriate group. No live deploy.

After slice 6 lands: the testbed OPNsense produces the same `vtysh -c "show interfaces"` and `pfctl -s rules` shape as prod, modulo testbed-specific addresses. The MWN1 daemon validates `Version` and `ReadConfigXML`. The config import gate from `opnsense-testbed-config-import.md` runs to completion with no rollback.

After all slices land: a separate ticket files for the actual 26.x testbed upgrade. That ticket consumes this parity work as its prerequisite.

## Risk callouts

1. **Wedged VM 101.** The current testbed VM 101 is unreachable on `agoodkind@3d06:bad:b01:200::11`. The from-scratch runbook builds a NEW VM (e.g. VM 102) so the wedged VM 101 stays as a forensic artifact for diagnosing the MWAN-119 v2 failure. Do not retire it until the new baseline is verified.
2. **Testbed addressing collision.** Prod uses 10.250.0.0/16 and 3d06:bad:b01::/56 broadly. Testbed uses 10.240.0.0/16 and 3d06:bad:b01:200..209::/56. Slice 4's transform must enforce this split or risk emitting routes that conflict with the prod plane.
3. **WireGuard peer collision.** Prod and testbed must not share peer keys, or a misrouted handshake from one side could land at the other. Slice 4 substitutes peer keys.
4. **No PCI passthrough on suburban.** The slice 4 `config.xml` transform handles the asymmetry by remapping every prod-side `iavf0` reference to the testbed's matching device. `MWAN-148` chose this over the rejected FreeBSD `rc.conf` rename approach because device names need not match prod (only interface order matters) and centralizing the remap in the transform layer keeps FreeBSD stock.

## Out of scope

- The actual 26.x OPNsense upgrade. Files separately once parity lands.
- Email unification (`MWAN-132`).
- BGP graceful restart (`MWAN-130`).
- Converting static lxc-100 and lxc-116 configs to `.j2` (`MWAN-22`, replaced by env injection in `MWAN-131`).

## Recovery anchor

If context is lost:
- Plan: `mwan/docs/MWAN-140-testbed-infra-parity-plan.md` (this file).
- Parent ticket: `MWAN-140`. Children file post-approval.
- Adjacent plans: `MWAN-132` email unification at `mwan/docs/MWAN-132-email-unification-plan.md`. `MWAN-130` BGP graceful restart at `mwan/docs/MWAN-130-bgp-graceful-restart-plan.md`.
- Runbooks: `mwan/docs/runbooks/opnsense-serial-vm-from-scratch.md` and `mwan/docs/runbooks/opnsense-testbed-config-import.md`. Both committed on local main.
- The wedged testbed VM 101 stays in place as forensic evidence until slice 6 produces a verified replacement.
