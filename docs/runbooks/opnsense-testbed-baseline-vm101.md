# OPNsense Testbed VM 101 Current State

This file records the current OPNsense testbed VM `101` (`opnsense-test`)
on suburban. Use [docs/runbooks/opnsense-testbed-config-import.md](opnsense-testbed-config-import.md)
for the current config-import gate, and use [docs/infra/mwan-layout.md](../infra/mwan-layout.md) for broader MWAN
topology.

## Current State

- VM `101` (`opnsense-test`) is the current suburban OPNsense testbed VM.
- The OPNsense management interface is `vtnet0` on `vmbrtrunk` untagged.
- VM 101 management addresses are `10.240.4.1/24` and
  `3d06:bad:b01:204::1/64`.
- The suburban `vmbrtrunk` stub addresses are `10.240.4.5/24` and
  `3d06:bad:b01:204::5/64`.
- The host-side OPNsense gRPC target is
  `unix:///var/run/qemu-server/101.mwanrpc`.
- The named virtio-console port is `io.goodkind.mwan-opnsense.0`.
- The guest-side daemon serial path is `/dev/ttyV0.1` when
  `/dev/vtcon/io.goodkind.mwan-opnsense.0` points there.

## Current Checks

Verify the guest-side virtio-console symlink before assuming the serial
device:

```sh
ls -l /dev/vtcon/io.goodkind.mwan-opnsense.0
```

If the symlink points at `/dev/ttyV1.1`, update
`mwan_opnsense_listen_serial` before starting `mwan_opnsense`.

Verify the host-side OPNsense gRPC path from suburban:

```sh
mwan opnsense version -target unix:///var/run/qemu-server/101.mwanrpc
```

The command should return the daemon build banner.

## Current Reset Rule

Use Proxmox snapshot rollback for VM 101 reset. Do not use snapshots
created with `--vmstate 1`, because RAM snapshots can resume stale
network and clock state.
