# MWAN host layout

MWAN runs as one Go binary spread across a few hosts, and each host runs only the subcommands its role needs. Production runs on the vault hypervisor in San Francisco, and the suburban hypervisor in New Jersey runs a testbed that mirrors it, with the same roles on matching guests.

## Roles and their command surface

The `mwan` binary is a monolith whose subcommands each do one job, and a host runs the subset its role requires.

- The MWAN VM is the WAN router. It runs `mwan agent`, the gRPC service that drives the embedded BGP speaker and applies health-driven route decisions, under the `mwan-agent.service` unit.
- The failover LXC is the backup BGP peer. It runs `mwan agent` and `mwan ifmgr`, the interface manager that applies interface-mode configuration read from `/etc/mwan/config.toml`, under the `mwan-agent.service` and `mwan-ifmgr.service` units.
- The Proxmox host watches and recovers the VM from outside it. It runs `mwan ifmgr` for its own out-of-band interface and `mwan watchdog`, the daemon that probes connectivity and rolls the VM back to a known-good snapshot when a change breaks it. The testbed host additionally runs `mwan opnsense host serve`, the Unix-socket bridge to the testbed OPNsense serial channel.
- The OPNsense VM runs `mwan opnsense serve`, the FreeBSD daemon that edits `config.xml` over the serial channel. It has no `/etc/mwan/`; its settings live in `rc.conf.d`.

The ISP-simulator containers and the unrelated service containers on these hosts run no MWAN command.

## Binary rollout order

Roll a new MWAN binary onto the testbed first and production second, and verify each host before moving to the next.

1. suburban host
2. testbed MWAN VM
3. testbed failover LXC
4. testbed OPNsense
5. production failover LXC
6. production MWAN VM
7. vault host
8. production OPNsense

A production step needs a live verification and a saved rollback copy of the binary before the swap.

## WAN links and priority

MWAN load-balances three wide-area networks and fails over between them by BGP route priority. Webpass is primary, AT&T is secondary, and Monkeybrains is the lossy tertiary fallback.

- `enwebpass0` carries Webpass, a Google Fiber line, as the primary path. It takes a dynamic carrier-grade NAT IPv4 address and a provider-delegated IPv6 prefix.
- `enatt0.3242` carries AT&T over an 802.1X-authenticated VLAN as the secondary path, and takes a dynamic IPv4 address and an AT&T-delegated IPv6 prefix. Its IPv6 gateway answers while internet IPv6 over it does not, which points at a network-prefix-translation or prefix-delegation routing fault on that path.
- `enmbrains0` carries Monkeybrains as the tertiary fallback. It takes a public static IPv4 address, a SLAAC IPv6 address, and a DHCPv6 prefix delegation, and MWAN maps its internal IPv6 range onto the first block of that delegation with network prefix translation. The delegation renumbers, so `find-pd-prefixes.sh` reads the live prefix rather than a stored one. MWAN health-checks this path and excludes it from alerts because it is lossy.

The untagged parent interface `enatt0` carries the management link to the AT&T optical network terminal and is the Layer 2 parent of the tagged `enatt0.3242`.

## AT&T ONT access

The AT&T optical network terminal, the device that ends the fiber, is a Realtek GPON module that MWAN sees as a Layer 2 device on the untagged `enatt0` interface. MWAN reaches its management plane at `192.168.1.1`.

The terminal keeps the stock vendor defaults for this Humax module, and those credentials are not stored in the repo, so get them from the operator password manager. It runs dropbear 0.48, which requires legacy key-exchange, host-key, and cipher options, and it also exposes telnet on port 23.

Reach it over SSH from the MWAN VM, replacing `<user>` and supplying the password when prompted.

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
  -o KexAlgorithms=+diffie-hellman-group1-sha1 \
  -o HostKeyAlgorithms=+ssh-rsa \
  -o Ciphers=+3des-cbc \
  <user>@192.168.1.1
```

The terminal exposes diagnostic tools such as `ShowStatus`, `pondetect`, `checkomci`, `omci_app`, `omcicli`, and `oamcli`, and status files under `/proc` such as `/proc/omci`, `/proc/fiber_debug`, `/proc/fiber_mode`, and `/proc/internet_flag`. It accepts only a few connections at once and wedges its SSH and telnet daemons when hit rapidly, so keep to one connection at a time. If both daemons stop answering while TCP still accepts, reboot the terminal over SSH.

## AT&T 802.1X authentication

AT&T authenticates the line with 802.1X, so the MWAN VM holds AT&T's certificates and runs `wpa_supplicant` directly while the terminal forwards the authentication frames at Layer 2. The bring-up runs as a chain of systemd units.

1. `wpa_supplicant-mwan.service` runs the supplicant against `enatt0` using the certificates in `/etc/wpa_supplicant/`.
2. When the supplicant reaches the authenticated state, `wpa-cli-action.service` writes the marker file `/run/wpa_supplicant-mwan.authenticated`.
3. `wpa-authenticated.path` watches that marker and starts `wpa-authenticated.service`, which starts `bringup-att-vlan.service`.
4. `bringup-att-vlan.sh` waits for authentication, then runs `networkctl renew enatt0.3242` to trigger DHCPv4 and DHCPv6 prefix delegation on the VLAN.
5. DHCPv4 yields the AT&T public address, and DHCPv6 yields the delegated IPv6 prefix that the network-prefix-translation rules map onto.

The failures you meet, with their recovery, are these.

- `wpa-authenticated.path` sits in `failed (unit-start-limit-hit)` when the path started `bringup-att-vlan` too many times at boot. Clear it with `systemctl reset-failed wpa-authenticated.path`, then `systemctl start wpa-authenticated.path`.
- A completed TLS handshake followed by `EAP-Failure` means AT&T's authentication server rejected the certificate identity. The supplicant port can stay authorized from the boot-time authentication while DHCP gets no replies, because the authentication session is invalid. Reboot the terminal over SSH to clear AT&T's session state; it returns in about thirty seconds and re-presents its authentication frames, which starts a fresh session.
- `Supplicant PAE state=AUTHENTICATING` with `wpa_state=COMPLETED` means re-authentication is failing in the background, and the same terminal reboot recovers it.

Inspect the chain on the MWAN VM.

```bash
ls -la /run/wpa_supplicant-mwan.authenticated
ls -la /etc/wpa_supplicant/*.pem
sudo wpa_cli -i enatt0 status
journalctl -u wpa_supplicant-mwan --no-pager --since "1 hour ago"
journalctl -u bringup-att-vlan --no-pager --since "1 hour ago"
```
