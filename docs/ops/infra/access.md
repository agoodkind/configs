# SSH and host access

Direct SSH is the normal path to every routed host. WireGuard and Cloudflare
private access carry the same homelab routes, so the target does not change
between them.

## 1. Connect directly

Use the fully qualified domain name when private DNS resolves it. Otherwise,
look up the host's IPv6 address in
[the service inventory](../../../ansible/inventory/group_vars/all/service_mapping.yml).
Both paths connect directly.

```bash
ssh root@<service-hostname-or-ipv6>
ssh agoodkind@<opnsense-hostname-or-ipv6>
```

The proxy container runs OpenSSH on port 2222 because SSHPiper owns port 22:

```bash
ssh -p 2222 root@proxy.home.goodkind.io
```

## 2. Connect through SSHPiper

SSHPiper is the fallback when the proxy is reachable but direct SSH is not.
It routes the first SSH username component to a managed service target:

```bash
ssh <route-name>@ssh.home.goodkind.io
```

Reach the proxy container itself through SSHPiper with the `@proxy` suffix:

```bash
ssh root@proxy@ssh.home.goodkind.io
```

## 3. Use break-glass access

Use an out-of-band path when the target network or SSH daemon is unavailable.
Look up the guest identifier in the service inventory first.

Run a command inside an LXC container or QEMU virtual machine from its Proxmox
host:

```bash
ssh root@<proxmox-host> 'pct exec <vmid> -- <command>'
ssh root@<proxmox-host> 'qm guest exec <vmid> -- <command>'
```

For a QEMU virtual machine with a configured serial console:

```bash
ssh -t root@<proxmox-host> 'qm terminal <vmid>'
```

OPNsense also has a serial control channel; see
[the OPNsense out-of-band daemon](../opnsense/daemon.md). For hypervisor
recovery, see [emergency out-of-band access](oob.md).

## Diagnostics-only SSH options

Disable strict host key checking only for automation or diagnostics:

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@<host>
```

## SSH user conventions

- Proxmox hosts and Linux guests use `root`.
- OPNsense uses `agoodkind`; run privileged commands with `sudo`.
- The proxy container uses `root` on direct port 2222.

## SSH from automation

Quote a remote command in single quotes so the local shell passes it
verbatim:

```bash
ssh host 'somecommand --flag=value[index]'
```

For a longer command, write it to a temporary file, copy it to the host, run
it, and remove it afterward.
