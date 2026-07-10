# OPNsense testbed baseline

Current state for the OPNsense testbed VM (`opnsense-test`) on suburban. Use
[docs/opnsense/testbed-config-import.md](testbed-config-import.md) for the
config-import gate. Broader testbed topology lives in
[docs/infra/suburban-testbed.md](../infra/suburban-testbed.md). The MWAN
runtime story lives in [docs/mwan/overview.md](../mwan/overview.md). Local
browser forwarding for headless Chrome or Playwright lives in
[docs/opnsense/ui-testing.md](ui-testing.md).

## Current state

- VM `101` (`opnsense-test`) is the suburban OPNsense testbed VM.
- The OPNsense VM has one NIC backed by the `vmbrtrunk` VLAN-aware bridge.
  FreeBSD names it `vtnet0`.
- The MANAGEMENT interface (`opt9` in `config.xml`) carries `10.240.4.1/24` and
  `3d06:bad:b01:204::1/64`. Suburban joins the same broadcast domain via a
  `vmbrtrunk` stub at `10.240.4.5/24` and `3d06:bad:b01:204::5/64`, defined in
  [opentofu/suburban/networks.tf](../../opentofu/suburban/networks.tf).
- The LAN interface (`lan` in `config.xml`) carries `192.168.1.1/24` and
  `3d06:bad:b01:211::1/64`.
- The WAN/internal interface carries `10.250.250.2/29` and
  `3d06:bad:b01:201::2/64`. Suburban reaches that interface through `vmbr2`,
  and TCP port 22 is open there.
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
state. See [docs/opnsense/operational-notes.md](operational-notes.md) for the
full snapshot rule and the post-rollback verification list.
