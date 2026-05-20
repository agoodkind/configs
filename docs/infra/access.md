# SSH and host access

How to reach hosts in the homelab and which entry point to prefer. Live IPs for
each host belong in [docs/infra/overview.md](overview.md) and
[docs/infra/hosts.md](hosts.md), not here.

## Proxy container access

The proxy container has two SSH entry points:

- **Port 22 (SSHPiper)**: public-facing. Routes traffic to other containers by
username pattern.
- **Port 2222 (OpenSSH)**: admin-facing. Direct access to the proxy container
itself.

### Proxy shortcut

To reach the proxy container via the public port (SSHPiper), use the
`@proxy` suffix when configured, or the matching `~/.ssh/config` alias:

```bash
ssh root@proxy@ssh.home.goodkind.io
# Or, with ~/.ssh/config:
ssh root@proxy
```

This matches the `^(.+)@proxy` regex in `sshpiperd.yaml` and is routed to
`[::1]:2222`.

## Primary method: SSHPiper routing

All `*.ssh.home.goodkind.io` DNS resolves to the proxy host, where SSHPiper
routes by username:

```bash
ssh adguard@ssh.home.goodkind.io
ssh pdns@ssh.home.goodkind.io
ssh mwan@ssh.home.goodkind.io
```

Pattern: `ssh <short-hostname>@ssh.home.goodkind.io`. The short hostname is the
first label of the full container name (`adguard` from
`adguard.home.goodkind.io`).

## Direct IP access (bypass SSHPiper)

When SSHPiper is unavailable or for troubleshooting, use IPs directly. Prefer
IPv6.

```bash
ssh root@<ipv6-address>
```

### Finding a container or VM IP

```bash
# 1. SSH to the Proxmox host that owns the guest.
# 2. Find the guest by name.
pct list | grep -i <name>            # LXC containers
qm list | grep -i <name>             # QEMU VMs

# 3. Read its primary IP. Prefer inet6.
pct exec <vmid> -- ip addr show eth0
qm guest cmd <vmid> network-get-interfaces

# 4. SSH directly. Prefer IPv6.
ssh root@<ipv6-address>
```

## Jump host access (last resort)

If direct IP access fails, jump through OPNsense, then to the hypervisor, then
to the target:

```bash
ssh agoodkind@<opnsense>
# from there:
ssh root@<proxmox-host>
ssh root@<container-ip>
```

## Diagnostics-only SSH options

Disable strict host key checking for automation or diagnostics only:

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@<ip>
```

## Direct access to the proxy host

To reach the proxy host itself (bypassing SSHPiper):

```bash
ssh -p 2222 root@<proxy-ipv6>
```

## SSH user conventions

- Proxmox hosts: `root` over IPv6.
- LXC containers: `root` over IPv6 via SSHPiper or direct.
- OPNsense (FreeBSD): `agoodkind`. Use `sudo` for privileged tasks.
- Proxy host (port 2222): `root`.
- Ansible controller container: has `PROXMOX_API_TOKEN` available.

## SSH from automation (zsh quoting)

When passing a command to `ssh host <command>`, quote the entire remote command
string in single quotes so that the local shell passes the argument verbatim
without glob expansion:

```bash
ssh host 'somecommand --flag=value[index]'
```

For anything more complex than a single command, write the logic to a temp
file via `mktemp`, `scp` it to the host, execute it there, and clean up
afterward.
