# SSH and host access

Direct SSH is the normal path to every routed host. WireGuard and Cloudflare
private access carry the same homelab routes, so the target does not change
between them.

## Connect directly

Use the fully qualified domain name when private DNS resolves it. Otherwise,
look up the host's IPv6 address in its
[native feature configuration](../../../opentofu/modules).
Both paths connect directly.

```bash
ssh root@<service-hostname-or-ipv6>
ssh agoodkind@<opnsense-hostname-or-ipv6>
```

## Use break-glass access

Use an out-of-band path when the target network or SSH daemon is unavailable.
Read the guest identifier from its native definition first.

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

For OPNsense serial access, see
[the OPNsense out-of-band daemon](../opnsense/daemon.md).

## Diagnostics-only SSH options

Disable strict host key checking only for automation or diagnostics:

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@<host>
```

## SSH user conventions

- Proxmox hosts and Linux guests use `root`.
- OPNsense uses `agoodkind`; run privileged commands with `sudo`.

## SSH from automation

Quote a remote command in single quotes so the local shell passes it
verbatim:

```bash
ssh host 'somecommand --flag=value[index]'
```

For a longer command, write it to a temporary file, copy it to the host, run
it, and remove it afterward.
