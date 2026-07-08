# OPNsense testbed baseline

Current state for the OPNsense testbed VM (`opnsense-test`) on suburban. Use
[docs/opnsense/testbed/import.md](import.md) for the config-import gate. Broader
testbed topology lives in [docs/mwan/testbed.md](../../mwan/testbed.md), the MWAN
runtime story in [docs/mwan/overview.md](../../mwan/overview.md), and local browser
forwarding for headless Chrome or Playwright in [docs/opnsense/ui.md](../ui.md).

## Current state

The testbed OPNsense guest and its bridges are provisioned by
[opentofu/suburban/](../../../opentofu/suburban/), which owns the VM id, the
`vmbrtrunk` NIC, and the suburban-side stub. Its interface addresses come from the
imported `config.xml`. This page states the structural shape, not the literal VMID
or management, LAN, and WAN addresses, which live in those sources:

- The guest has one NIC on the `vmbrtrunk` VLAN-aware bridge; FreeBSD names it
  `vtnet0`.
- The MANAGEMENT interface (`opt9` in `config.xml`) shares its broadcast domain
  with a `vmbrtrunk` stub on suburban defined in
  [opentofu/suburban/networks.tf](../../../opentofu/suburban/networks.tf).
- The LAN interface (`lan`) and the WAN/internal interface carry the imported
  testbed addresses. Suburban reaches the WAN/internal interface through `vmbr2`
  with TCP port 22 open.
- The host-side OPNsense gRPC socket, the named virtio-console port, and the guest
  serial path are the daemon transport contract, fixed by the VM `args` in
  [testbed/vm-101/qm-config.md](../../../testbed/vm-101/qm-config.md).

## Pre-flight checks

The commands below target the testbed OPNsense VM whose id and `args` live in
[opentofu/suburban/](../../../opentofu/suburban/) and
[testbed/vm-101/qm-config.md](../../../testbed/vm-101/qm-config.md); they use its
current id `101`.

Verify the guest-side virtio-console symlink before assuming the serial device:

```sh
ls -l /dev/vtcon/io.goodkind.mwan-opnsense.0
```

If the symlink points at `/dev/ttyV1.1`, update `mwan_opnsense_listen_serial`
before starting `mwan_opnsense`.

Verify the host-side OPNsense gRPC path from suburban:

```sh
mwan opnsense version -target unix:///var/run/qemu-server/101.mwanrpc
```

The command should return the daemon build banner.

## Reset rule

Reset the testbed OPNsense by Proxmox snapshot rollback. The snapshot rule (no
saved RAM) and the post-rollback verification list are in
[docs/opnsense/notes.md](../notes.md).
