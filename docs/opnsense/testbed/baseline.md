# OPNsense testbed baseline

Current state for the OPNsense testbed VM (`opnsense-test`) on suburban. Use
[docs/opnsense/testbed/import.md](import.md) for the
config-import gate. Broader testbed topology lives in
[docs/mwan/testbed.md](../../mwan/testbed.md). The MWAN
runtime story lives in [docs/mwan/overview.md](../../mwan/overview.md). Local
browser forwarding for headless Chrome or Playwright lives in
[docs/opnsense/ui.md](../ui.md).

## Current state

The guest is `opnsense_test` in
[service_mapping.yml](../../../ansible/inventory/group_vars/all/service_mapping.yml),
which owns its VMID and addresses.

- The OPNsense VM has one NIC backed by the `vmbrtrunk` VLAN-aware bridge.
  FreeBSD names it `vtnet0`.
- The VMNET interface (`opt6` in `config.xml`) is the guest segment, and every
  testbed guest uses it as their default route. Suburban joins the same
  broadcast domain through a `vmbrtrunk` stub declared in
  [opentofu/suburban/networks.tf](../../../opentofu/suburban/networks.tf).
- The LAN interface (`lan` in `config.xml`) carries `192.168.1.1/24` and
  `3d06:bad:b01:211::1/64`.
- The WAN/internal interface carries `10.250.250.2/29` and
  `3d06:bad:b01:201::2/64`. Suburban reaches that interface through `vmbr2`,
  and TCP port 22 is open there. This is also the BGP peering segment, and it
  mirrors production's rather than following the VMID scheme.
- The host-side OPNsense gRPC target is a socket named for the guest's VMID,
  under `/var/run/qemu-server/`. Ansible renders it from `mwan_opnsense_vmid`.
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

Verify the host-side OPNsense gRPC path from suburban. The socket is named for
the guest's VMID, so read it from the rendered config rather than typing it:

```sh
ssh suburban 'mwan opnsense version -target "$(sed -n "s/^chardev = \"\(.*\)\"/\1/p" /etc/mwan/config.toml)"'
```

The command should return the daemon build banner.

## Reset rule

Reset the OPNsense guest by rolling back a Proxmox snapshot. Do not use snapshots created
with `--vmstate 1`, because RAM snapshots can resume stale network and clock
state. See [docs/opnsense/operations.md](../operations.md) for the
full snapshot rule and the post-rollback verification list.
