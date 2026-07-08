# MWAN Layout

This page maps each MWAN host to its command surface, unit files, repo source,
config template, role, and management address. The state below was captured on
2026-05-07 against main commit `4c754f4`. Each management address matches
`[main].mwan_mgmt_addr` in that host's `/etc/mwan/config.toml`.

| Host | OS | MWAN command surface | Unit files on host | Repo source | Config template | Role | VMID | Management access |
| ---- | -- | -------------------- | ------------------ | ----------- | --------------- | ---- | ---- | ----------------- |
| vault, the San Francisco Proxmox host | Linux/amd64 | `mwan ifmgr`, `mwan watchdog` | `mwan-ifmgr.service`, `mwan-watchdog.service` | [mwan/go/cmd/mwan/mwan-ifmgr.service](../../mwan/go/cmd/mwan/mwan-ifmgr.service); watchdog unit lives only on host | [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | `vault-oob` | 113 | `3d06:bad:b01::254` |
| mwan VM 113 on vault | Linux/amd64 | `mwan agent` | `mwan-agent.service` | [mwan/go/cmd/mwan/mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service) | [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | agent host | 113 | `3d06:bad:b01::113` |
| mwan-failover LXC 116 on vault | Linux/amd64 | `mwan agent`, `mwan ifmgr` | `mwan-agent.service`, `mwan-ifmgr.service` | [mwan/go/cmd/mwan/mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service), [mwan-failover/mwan-ifmgr.service](../../mwan-failover/mwan-ifmgr.service) | [mwan-failover/config.toml.j2](../../mwan-failover/config.toml.j2) | `lxc-failover-backup` | 116 | reachable from vault with `pct exec 116` |
| OPNsense VM 101 on vault | FreeBSD 14.3 | `mwan opnsense serve` | `/usr/local/etc/rc.d/mwan_opnsense`, enabled by `/etc/rc.conf.d/mwan_opnsense` | [mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense](../../mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense) | no `/etc/mwan/`; settings live in `rc.conf.d` | router helper | 101 | `agoodkind@3d06:bad:b01::1` through vault |
| suburban, the New Jersey Proxmox testbed host | Linux/amd64 | `mwan ifmgr`, `mwan opnsense host serve`, `mwan watchdog` | `mwan-ifmgr.service`, `mwan-opnsense-host.service`, `mwan-watchdog-testbed.service` | [mwan/go/cmd/mwan/mwan-ifmgr.service](../../mwan/go/cmd/mwan/mwan-ifmgr.service), [mwan/go/cmd/mwan/mwan-opnsense-host.service](../../mwan/go/cmd/mwan/mwan-opnsense-host.service); watchdog-testbed unit lives only on host | [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | `suburban-wg` | 950 | `suburban` SSH alias |
| testbed VM 950 on suburban | Linux/amd64 | `mwan agent` | `mwan-agent.service` | [mwan/go/cmd/mwan/mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service) | [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | agent host | 950 | `3d06:bad:b01:204::950` on the vmbrtrunk 204:: services LAN |
| testbed LXC 100 on suburban | Linux/amd64 | `mwan agent`, `mwan ifmgr` | `mwan-agent.service`, `mwan-ifmgr.service` | [mwan/go/cmd/mwan/mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service), [mwan-failover/mwan-ifmgr.service](../../mwan-failover/mwan-ifmgr.service) | [mwan-failover/config.toml.j2](../../mwan-failover/config.toml.j2) | `lxc-failover-backup` | 100 | reachable from suburban with `pct exec 100` |
| testbed LXCs 200, 201, and 202 on suburban | Linux/amd64 | none | none | none | none | ISP simulators | n/a | reachable from suburban with `pct exec` |
| tack LXC 117 on vault | Linux/amd64 | none | none | none | none | unrelated service container | 117 | `tack` SSH alias |

Current tracked layout:

| Path | Current purpose |
| ---- | --------------- |
| [mwan/](../../mwan/) | Linux MWAN VM runtime files and the [mwan/go/](../../mwan/go/) monolith source tree |
| [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | Unified Linux MWAN VM TOML template for production VM 113 and testbed VM 950 |
| [mwan-failover/](../../mwan-failover/) | Shared failover LXC artifacts for production LXC 116 and testbed LXC 100 |
| [mwan-failover/sysctl.conf](../../mwan-failover/sysctl.conf) | Canonical failover LXC sysctl file, including IPv6 forwarding and router-advertisement acceptance |
| [testbed/](../../testbed/) | Canonical testbed topology assets, OPNsense test files, ISP LXC files, and VM 950 snippets |
| [proxmox/](../../proxmox/) | Canonical Proxmox host artifacts, including host-side watchdog files and host config snippets |
| [proxmox/config/10-mwan-retention.conf](../../proxmox/config/10-mwan-retention.conf) | Canonical vault journald retention file |
| [docs/](../../docs/) | Canonical documentation location |
| [opentofu/](../../opentofu/) | OpenTofu configuration for provisioned containers and VMs |

## MWAN binary rollout order

Roll a manual MWAN binary out on the testbed first, then production. Verify each
host before moving to the next, in this order:

1. suburban host
2. testbed VM 950
3. testbed LXC 100
4. testbed OPNsense
5. production LXC 116
6. production VM 113
7. vault
8. production OPNsense

A production step needs a live surgical verification and a rollback copy before
the binary swap.

## MWAN WAN Links

| Interface | Provider | IPv4 | IPv6 | Route metric | Notes |
| --------- | -------- | ---- | ---- | ------------ | ----- |
| `enwebpass0` | Webpass | `dynamic/CGNAT (not recorded)` | `delegated /64 from provider (not recorded)` | 10 (primary) | Google Fiber. RTT to `2001:4860:4860::8888` ~2.6 ms. |
| `enatt0.3242` | AT&T (802.1X) | `dynamic/CGNAT (not recorded)` | Provider-delegated IPv6 from AT&T (not recorded) | 1024 (secondary) | IPv6 gateway pings fine but `ping6 8.8.8.8` is 100% loss. NPT rule or PD routing issue suspected. |
| `enatt0` (parent) | AT&T mgmt to ONT | `192.168.1.2/24` (link to ONT) | n/a | n/a | Untagged parent of `enatt0.3242`. Hosts the link to the AT&T ONT at `192.168.1.1`. |
| `enmbrains0` | Monkeybrains | `158.247.70.19/26` (public) | SLAAC `2607:f598:d3e0:131::/64` plus a DHCPv6-PD `/56`, currently `2607:f598:d3e8:4500::/56` | 5000 (tertiary) | NPT maps the internal `/60` onto the PD's first `/60`. The PD renumbers, so `find-pd-prefixes.sh` reads the live delegation and the literal here is only the value at last check. Health-checked but excluded from alerts as a lossy fallback. |

## AT&T ONT access

The AT&T ONT is a Realtek-based GPON SFP ("ONT-on-a-stick", firmware
`V1.0-220923`) presented to MWAN as a Layer-2 device on the untagged
parent `enatt0`. MWAN reaches its management plane at `192.168.1.1` over
that link.

- Credentials are the stock vendor defaults for this Humax SFP unit. They are not stored in the repo, so use operator memory or the Humax operator doc.
- SSH: dropbear 0.48, requires legacy KEX, host key, and cipher.
- Telnet: also open on port 23.

SSH command (from MWAN, replace `<user>` and supply the password interactively):

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  -o KexAlgorithms=+diffie-hellman-group1-sha1 \
  -o HostKeyAlgorithms=+ssh-rsa \
  -o Ciphers=+3des-cbc \
  <user>@192.168.1.1
```

Useful binaries on the ONT: `ShowStatus`, `pondetect`, `checkomci`,
`omci_app`, `omcicli`, `oamcli`. Useful `/proc` entries: `/proc/omci`,
`/proc/fiber_debug`, `/proc/fiber_mode`, `/proc/internet_flag`. The
device has a small connection limit and wedges its SSH/telnet daemons
when hit rapidly; keep to one connection at a time and `reboot` over
SSH if both daemons stop responding while TCP accepts succeed.

## AT&T 802.1X chain on MWAN

The MWAN VM holds AT&T's certificates and runs `wpa_supplicant` directly,
with the ONT acting only as a Layer-2 EAPOL forwarder. The bring-up chain:

1. `wpa_supplicant-mwan.service` runs the supplicant against the parent
   `enatt0` interface using certs at
   `/etc/wpa_supplicant/{ca_cert,client_cert,private_key}.pem`.
2. On reaching the AUTHENTICATED state, `wpa-cli-action.service` writes
   `/run/wpa_supplicant-mwan.authenticated`.
3. `wpa-authenticated.path` watches that file and starts
   `wpa-authenticated.service`, which then starts
   `bringup-att-vlan.service`.
4. `bringup-att-vlan.sh` polls `wpa_cli status` until AUTHENTICATED, then
   runs `networkctl renew enatt0.3242` to trigger DHCPv4 + DHCPv6-PD on
   the VLAN sub-interface.
5. DHCPv4 yields the AT&T public address. DHCPv6-PD yields the AT&T
   delegated `/60` used by `update-npt.sh`.

Failure modes:

- **`wpa-authenticated.path` in `failed (unit-start-limit-hit)`**: the
  path triggered `bringup-att-vlan` too many times in a row at boot.
  Clear with `systemctl reset-failed wpa-authenticated.path` then
  `systemctl start wpa-authenticated.path`.
- **TLS handshake completes but AT&T returns `EAP-Failure`**: AT&T's
  RADIUS rejected our cert identity. The supplicant port may stay
  `Authorized` from the initial boot auth, but DHCP gets no replies
  because the RADIUS session is invalid. Reboot the ONT
  (`reboot` over SSH at `192.168.1.1`) to clear AT&T's session state;
  the ONT comes back in ~30s and re-presents EAPOL, triggering a fresh
  RADIUS session.
- **`Supplicant PAE state=AUTHENTICATING` with `wpa_state=COMPLETED`**:
  re-authentication failing in the background. Same recovery as above.

Diagnostic commands on MWAN:

```bash
ls -la /run/wpa_supplicant-mwan.authenticated
ls -la /etc/wpa_supplicant/*.pem
sudo wpa_cli -i enatt0 status
journalctl -u wpa_supplicant-mwan --no-pager --since "1 hour ago"
journalctl -u bringup-att-vlan --no-pager --since "1 hour ago"
networkctl status enatt0.3242
```
