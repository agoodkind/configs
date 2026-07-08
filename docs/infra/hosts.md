# Hosts Not On Vault Proxmox

This file tracks host state that does not belong under the vault hypervisor
inventory. These hosts are outside the dynamic inventory, so the addresses below
are a manual snapshot rather than a config-owned value; where a host is
Ansible-managed, its canonical hostname and IPv6 live in
[service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml).
For suburban guest inventory and bridge layout, use
[docs/mwan/testbed.md](../mwan/testbed.md).

| Host | OS / type | Network | Email setup | Ansible-managed? | Notes |
| ---- | --------- | ------- | ----------- | ---------------- | ----- |
| `home-assistant` | Home Assistant OS | `vlan0200`, `10.250.2.3` / `3d06:bad:b01:2::3` | N/A | No | KEA reservation confirmed. SSH on port `22222`. |
| `mini` | Ubuntu 24.04.4 LTS | `vlan0100`, `10.250.1.2` / `3d06:bad:b01:1::2` | Postfix with `send-email` at `/opt/scripts/send-email` | Partial | Has `scripts-updater.timer`. Needs [ansible/playbooks/prep-guests.yml](../../ansible/playbooks/prep-guests.yml). |
| `nas` | Ubuntu 24.04.3 LTS | `vlan0100`, `3d06:bad:b01:1::3` | Postfix with `send-email` at `/opt/scripts/send-email` | Partial | SSH via `ssh nas`. OPNsense alias `nas_host` points at `::3`. |
| `vault` | Debian 13, Proxmox VE | `3d06:bad:b01::254` | Postfix with `send-email` at `/opt/scripts/send-email` | No | [ansible/playbooks/deploy-consul-external.yml](../../ansible/playbooks/deploy-consul-external.yml) still carries a `consul_arch: arm64` bug for this host. |
| `suburban` | Debian 13, Proxmox VE | `3d06:bad:b01:200::1` on `vmbr1`, `10.240.0.148` on `vmbr0`, WireGuard `3d06:bad:b01:10::240` | Postfix with `send-email` at `/opt/scripts/send-email` | Partial | Remote NJ hypervisor. Bridge layout and guest set live in [docs/mwan/testbed.md](../mwan/testbed.md). SSH currently works by direct IPv4 to `root@10.240.0.148`. |
| `imac` | Intel iMac, macOS 18 | Comcast NJ LAN, reachable via suburban | macOS `send-email` | No | Not represented in current inventory. This entry remains a manual note until a source of truth exists. |
| `berylax` | OpenWrt 24.10.5, GL.iNet | Offline for now | `msmtp` with SMTP2GO wrappers in `/usr/local/bin/` | No | Historical host and serial-console notes live in [berylax.md](berylax.md). |
| `jetkvm` (x2) | JetKVM embedded Linux | Monkeybrains L2 segment | Unknown | No | Two KVM-over-IP devices on the Monkeybrains segment. They are not represented in inventory. |
