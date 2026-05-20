# MWAN Layout

Live MWAN state was captured on 2026-05-07 against main commit `4c754f4`.
Management addresses are the values used in `[main].mwan_mgmt_addr` in each
host's `/etc/mwan/config.toml`.

| Host | OS | MWAN command surface | Unit files on host | Repo source | Config template | Role | VMID | Management access |
| ---- | -- | -------------------- | ------------------ | ----------- | --------------- | ---- | ---- | ----------------- |
| vault, the San Francisco Proxmox host | Linux/amd64 | `mwan ifmgr`, `mwan watchdog` | `mwan-ifmgr.service`, `mwan-watchdog.service`; `mwan-oob.service` is disabled and stale | [mwan/go/cmd/mwan/mwan-ifmgr.service](../../mwan/go/cmd/mwan/mwan-ifmgr.service); watchdog unit lives only on host | [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | `vault-oob` | 113 | `3d06:bad:b01::254` |
| mwan VM 113 on vault | Linux/amd64 | `mwan agent` | `mwan-agent.service`; `mwan-health.service` is a legacy shell unit | [mwan/go/cmd/mwan/mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service) | [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | agent host | 113 | `3d06:bad:b01::113` |
| mwan-failover LXC 116 on vault | Linux/amd64 | `mwan agent`, `mwan ifmgr` | `mwan-agent.service`, `mwan-ifmgr.service` | [mwan/go/cmd/mwan/mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service), [mwan-failover/mwan-ifmgr.service](../../mwan-failover/mwan-ifmgr.service) | [mwan-failover/config.toml.j2](../../mwan-failover/config.toml.j2) | `lxc-failover-backup` | 116 | reachable from vault with `pct exec 116` |
| OPNsense VM 101 on vault | FreeBSD 14.3 | `mwan opnsense serve` | `/usr/local/etc/rc.d/mwan_opnsense`, enabled by `/etc/rc.conf.d/mwan_opnsense` | [mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense](../../mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense) | no `/etc/mwan/`; settings live in `rc.conf.d` | router helper | 101 | `agoodkind@3d06:bad:b01::1` through vault |
| suburban, the New Jersey Proxmox testbed host | Linux/amd64 | `mwan ifmgr`, `mwan opnsense host serve`, `mwan watchdog` | `mwan-ifmgr.service`, `mwan-opnsense-host.service`, `mwan-watchdog-testbed.service` | [mwan/go/cmd/mwan/mwan-ifmgr.service](../../mwan/go/cmd/mwan/mwan-ifmgr.service), [mwan/go/cmd/mwan/mwan-opnsense-host.service](../../mwan/go/cmd/mwan/mwan-opnsense-host.service); watchdog-testbed unit lives only on host | [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | `suburban-wg` | 950 | `suburban` SSH alias |
| testbed VM 950 on suburban | Linux/amd64 | `mwan agent` | `mwan-agent.service` | [mwan/go/cmd/mwan/mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service) | [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | agent host | 950 | `3d06:bad:b01:200::950` through suburban |
| testbed LXC 100 on suburban | Linux/amd64 | `mwan agent`, `mwan ifmgr` | `mwan-agent.service`, `mwan-ifmgr.service` | [mwan/go/cmd/mwan/mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service), [mwan-failover/mwan-ifmgr.service](../../mwan-failover/mwan-ifmgr.service) | [mwan-failover/config.toml.j2](../../mwan-failover/config.toml.j2) | `lxc-failover-backup` | 100 | reachable from suburban with `pct exec 100` |
| testbed LXCs 200, 201, 202, and 203 on suburban | Linux/amd64 | none | none | none | none | ISP simulators and proxy | n/a | reachable from suburban with `pct exec` |
| tack LXC 117 on vault | Linux/amd64 | none | none | none | none | unrelated service container | 117 | `tack` SSH alias |

Current tracked layout after the MWAN cleanup:

| Path | Current purpose |
| ---- | --------------- |
| [mwan/](../../mwan/) | Linux MWAN VM runtime files and the [mwan/go/](../../mwan/go/) monolith source tree |
| [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | Unified Linux MWAN VM TOML template for production VM 113 and testbed VM 950 |
| [mwan-failover/](../../mwan-failover/) | Shared failover LXC artifacts for production LXC 116 and testbed LXC 100 |
| [mwan-failover/sysctl.conf](../../mwan-failover/sysctl.conf) | Canonical failover LXC sysctl file, including IPv6 forwarding and router-advertisement acceptance |
| [testbed/](../../testbed/) | Canonical testbed topology assets, OPNsense test files, ISP LXC files, and VM 950 snippets |
| [proxmox/](../../proxmox/) | Canonical Proxmox host artifacts, including host-side watchdog files and host config snippets |
| [proxmox/config/10-mwan-retention.conf](../../proxmox/config/10-mwan-retention.conf) | Canonical vault journald retention file after the cleanup |
| [docs/](../../docs/) | Canonical documentation location after the docs move |
| [opentofu/](../../opentofu/) | OpenTofu configuration for provisioned containers and VMs |

OpenTofu variable cleanup is reflected in
[opentofu/variables.tf](../../opentofu/variables.tf) and
[opentofu/terraform.tfvars.example](../../opentofu/terraform.tfvars.example),
where the suburban Proxmox API token is represented as a variable and an
example placeholder rather than a plaintext secret. Preserve unrelated edits in
[opentofu/containers.tf](../../opentofu/containers.tf) unless the operator asks
for a separate OpenTofu cleanup.

Manual MWAN binary rollout remains testbed-first, then production. The order is
suburban host, testbed VM 950, testbed LXC 100, testbed OPNsense, production
LXC 116, production VM 113, vault, and production OPNsense. Production changes
need a live surgical verification step and a rollback copy before a binary
swap.

## MWAN repo drift to clean up

- [mwan/services/mwan-health.service](../../mwan/services/mwan-health.service)
  ships in the repo, while live VM 113 still has a legacy shell
  `mwan-health.service` that is not derived from the Go binary.
- [mwan/services/mwan-trace-boot.service](../../mwan/services/mwan-trace-boot.service),
  `mwan-update-att-pinned-dests.service`, `mwan-update-npt.service`, and
  `mwan-update-routes.service` still describe shell-script era services.
- `mwan cutover` and `mwan cutover2` are removed from the current binary, while
  stale wrappers may still exist on older production hosts.

## Stale MWAN binaries to clean up

- vault has stale `/usr/local/bin/mwan-cutover` and `/usr/local/bin/mwan-unfuck`
  files from April.
- production VM 113 has stale `/usr/local/bin/mwan-agent` and
  `/usr/local/bin/mwan-change-detect` files from March.
- production LXC 116 is clean and has only `/usr/local/bin/mwan` plus active
  service files.
- suburban has stale `/usr/local/bin/mwan-cutover`,
  `/usr/local/bin/mwan-watchdog`, `/usr/local/bin/mwan-watchdog-test`,
  `/usr/local/bin/mwan-unfuck`, and `/usr/local/bin/mwan-opnsense-host` files.
- testbed VM 950 is clean.
- testbed LXC 100 is clean.
- production OPNsense has timestamped `mwan-opnsense.pre-`* and
  `mwan_opnsense.pre-*` backup artifacts that are separate from the structured
  `.previous` rollback path.

## MWAN WAN Links

| Interface | Provider | IPv4 | IPv6 | Route metric | Notes |
| --------- | -------- | ---- | ---- | ------------ | ----- |
| `enwebpass0` | Webpass | `dynamic/CGNAT (not recorded)` | `delegated /64 from provider (not recorded)` | 10 (primary) | Google Fiber. RTT to `2001:4860:4860::8888` ~2.6 ms. |
| `enatt0.3242` | AT&T (802.1X) | `dynamic/CGNAT (not recorded)` | Provider-delegated IPv6 from AT&T (not recorded) | 1024 (secondary) | IPv6 gateway pings fine but `ping6 8.8.8.8` is 100% loss. NPT rule or PD routing issue suspected. |
| `enmbrains0` | Monkeybrains | `158.247.70.6/26` (public) | SLAAC `2607:f598:d3e0:131::/64` (no PD) | 5000 (tertiary) | RA restored. DHCPv6-PD not delegated (provider-side). NAT66 masquerade fallback active. IPv4 upgraded from CG-NAT to public. |
