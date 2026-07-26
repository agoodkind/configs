# OPNsense testbed baseline

Current state for the OPNsense testbed VM (`opnsense-test`) on suburban. Use
[docs/opnsense/testbed/import.md](import.md) for the
config-import gate. Broader testbed topology lives in
[docs/mwan/testbed.md](../../mwan/testbed.md). The MWAN
runtime story lives in [docs/mwan/overview.md](../../mwan/overview.md). Local
browser forwarding for headless Chrome or Playwright lives in
[docs/opnsense/ui.md](../ui.md).

## Current state

- VM `101` (`opnsense-test`) is the suburban OPNsense testbed VM.
- The OPNsense VM has one NIC backed by the `vmbrtrunk` VLAN-aware bridge.
  FreeBSD names it `vtnet0`.
- Every interface address comes from the config transform
  ([substitutions.yaml](../../../testbed/opnsense/substitutions.yaml)), which
  places the LAN `/64`s inside the aggregate MWAN translates so they reach native
  IPv6. Read that file for the current values.
- The MANAGEMENT interface (`opt9` in `config.xml`) is the services LAN. It holds
  the testbed guests (tack-qa, SeaweedFS, the DNS64 LXC, and VM 950 management),
  and suburban joins the same broadcast domain through a `vmbrtrunk` stub defined
  in [opentofu/suburban/networks.tf](../../../opentofu/suburban/networks.tf).
- The WAN/internal interface faces MWAN on `vmbr2` and carries the BGP transit
  session. Suburban reaches it there, and TCP port 22 is open on it.
- The host-side OPNsense gRPC target is
  `unix:///var/run/qemu-server/101.mwanrpc`.
- The named virtio-console port is `io.goodkind.mwan-opnsense.0`.
- The guest-side daemon serial path is `/dev/ttyV0.1` when
  `/dev/vtcon/io.goodkind.mwan-opnsense.0` points there.

## Pre-flight checks

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

Use Proxmox snapshot rollback for VM 101 reset. Do not use snapshots created
with `--vmstate 1`, because RAM snapshots can resume stale network and clock
state. See [docs/opnsense/operations.md](../operations.md) for the
full snapshot rule and the post-rollback verification list.
