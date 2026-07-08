# MWAN Layout

This page maps the MWAN host roles to their command surface, unit files, and repo
source. It does not repeat concrete VMIDs, management addresses, or per-host roles.
Those live in config: guest hostnames and IPv6 in
[service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml),
VMIDs in the per-hypervisor Proxmox inventory
([vault.proxmox.yml](../../ansible/inventory/vault.proxmox.yml),
[suburban.proxmox.yml](../../ansible/inventory/suburban.proxmox.yml)), and the ifmgr
role plus other per-host values in each group's
[group_vars](../../ansible/inventory/group_vars/). For a live host inventory, see
[docs/infra/vault.md](../infra/vault.md).

Production runs on the vault hypervisor and the testbed mirrors it on suburban, with
the same roles on matching guests:

| Role | Command surface | Unit files | Repo source | Config template |
| ---- | --------------- | ---------- | ----------- | --------------- |
| MWAN VM (agent host) | `mwan agent` | `mwan-agent.service` | [mwan-agent.service](../../mwan/go/cmd/mwan/mwan-agent.service) | [config.toml.j2](../../mwan/config/config.toml.j2) |
| Failover LXC | `mwan agent`, `mwan ifmgr` | `mwan-agent.service`, `mwan-ifmgr.service` | [mwan-failover/mwan-ifmgr.service](../../mwan-failover/mwan-ifmgr.service) | [mwan-failover/config.toml.j2](../../mwan-failover/config.toml.j2) |
| Proxmox host (OOB ifmgr + watchdog) | `mwan ifmgr`, `mwan watchdog` | `mwan-ifmgr.service`, watchdog unit (host-only) | [mwan-ifmgr.service](../../mwan/go/cmd/mwan/mwan-ifmgr.service) | [config.toml.j2](../../mwan/config/config.toml.j2) |
| OPNsense VM (router helper) | `mwan opnsense serve` | `rc.d/mwan_opnsense`, no `/etc/mwan/` | [rc.d/mwan_opnsense](../../mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense) | settings in `rc.conf.d` |
| Testbed host (suburban only) | adds `mwan opnsense host serve` | adds `mwan-opnsense-host.service` | [mwan-opnsense-host.service](../../mwan/go/cmd/mwan/mwan-opnsense-host.service) | [config.toml.j2](../../mwan/config/config.toml.j2) |

The ISP-simulator LXCs and unrelated service containers on these hosts run no MWAN
command surface.

Repo layout for these files:

| Path | Purpose |
| ---- | --------------- |
| [mwan/](../../mwan/) | Linux MWAN VM runtime files and the [mwan/go/](../../mwan/go/) monolith source tree |
| [mwan/config/config.toml.j2](../../mwan/config/config.toml.j2) | Unified Linux MWAN VM TOML template for the production and testbed MWAN VMs |
| [mwan-failover/](../../mwan-failover/) | Shared failover LXC artifacts for the production and testbed failover LXCs |
| [mwan-failover/sysctl.conf](../../mwan-failover/sysctl.conf) | Canonical failover LXC sysctl file, including IPv6 forwarding and router-advertisement acceptance |
| [testbed/](../../testbed/) | Canonical testbed topology assets, OPNsense test files, ISP LXC files, and testbed VM snippets |
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

Interface names and route metrics are config, rendered from group_vars into
[config.toml.j2](../../mwan/config/config.toml.j2); the priority order below is the
structural fact. Provider addresses and delegated prefixes are dynamic and are not
authoritative here: the live DHCPv6-PD is read by `find-pd-prefixes.sh`, and any
address shown is a last-checked example.

| Interface | Provider | Priority | Notes |
| --------- | -------- | -------- | ----- |
| `enwebpass0` | Webpass | primary | Google Fiber, dynamic CGNAT v4 and a provider-delegated v6 `/64`. |
| `enatt0.3242` | AT&T (802.1X) | secondary | Dynamic CGNAT v4 and an AT&T-delegated v6. IPv6 gateway pings fine but `ping6 8.8.8.8` is 100% loss; NPT rule or PD routing issue suspected. |
| `enatt0` (parent) | AT&T mgmt to ONT | n/a | Untagged parent of `enatt0.3242`. Carries the link to the AT&T ONT. |
| `enmbrains0` | Monkeybrains | tertiary | Public static v4, SLAAC v6, and a DHCPv6-PD `/56`. NPT maps the internal `/60` onto the PD's first `/60`; the PD renumbers, so `find-pd-prefixes.sh` reads the live delegation. Health-checked but excluded from alerts as a lossy fallback. |

## AT&T ONT access

The AT&T ONT is a Realtek-based GPON SFP ("ONT-on-a-stick", firmware
`V1.0-220923`) presented to MWAN as a Layer-2 device on the untagged
parent `enatt0`. MWAN reaches its management plane at `192.168.1.1` over
that link.

- Credentials are the stock vendor defaults for this Humax SFP unit. They are not stored in the repo, so get them from the operator password manager or the Humax operator doc.
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
