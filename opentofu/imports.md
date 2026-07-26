# OpenTofu import recipe for suburban resources

This file documents how an operator brings live suburban resources under
OpenTofu control without recreating them. The resources already exist on
suburban (`hypervisor.suburban.goodkind.io`); the goal is to attach Tofu
state to those running objects.

The root module keeps the backend and provider configuration. Host-specific
resources live under child modules:

- [suburban/](./suburban/) owns the suburban testbed bridges, VMs, and LXCs.
- [vault/](./vault/) owns production vault resources.

The bpg/proxmox provider import IDs follow these formats:

- Network interfaces: `<node_name>:<iface>`.
- VMs and containers: `<node_name>/<vm_id>`.

The suburban node name is `hypervisor`.

## Prerequisites

1. `terraform.tfvars` is populated with both Proxmox API tokens
   (`proxmox_api_token` for vault, `suburban_proxmox_api_token` for
   suburban). The example file at [terraform.tfvars.example](./terraform.tfvars.example)
   lists the fields. Real token values come from the Ansible vault, never the
   repo.
2. `tofu init` has run against the Consul backend at `[3d06:bad:b01::106]:8500`.
3. The live suburban shape matches the target resources before import:

```bash
ssh suburban 'pvesh get /nodes/hypervisor/network --output-format json'
ssh suburban 'qm config 113'
ssh suburban 'qm config 101'
ssh suburban 'pct config 116'
ssh suburban 'pct config 200'
ssh suburban 'pct config 201'
ssh suburban 'pct config 202'
```

## Network imports

Run from [opentofu/](./):

```bash
tofu import \
  'module.suburban.proxmox_network_linux_bridge.vm_management' \
  'hypervisor:vmbr1'

tofu import \
  'module.suburban.proxmox_network_linux_bridge.mwan_internal' \
  'hypervisor:vmbr2'

tofu import \
  'module.suburban.proxmox_network_linux_bridge.isp_webpass' \
  'hypervisor:vmbr4'

tofu import \
  'module.suburban.proxmox_network_linux_bridge.isp_att' \
  'hypervisor:vmbr5'

tofu import \
  'module.suburban.proxmox_network_linux_bridge.isp_mbrains' \
  'hypervisor:vmbr6'

tofu import \
  'module.suburban.proxmox_network_linux_bridge.trunk' \
  'hypervisor:vmbrtrunk'

tofu import \
  'module.suburban.proxmox_network_linux_vlan.trunk_vlan_100' \
  'hypervisor:vmbrtrunk.100'
```

## Guest imports

Run from [opentofu/](./):

Testbed VMIDs match production's, so a guest and its prod counterpart share a
number: the MWAN VM is 113, the failover LXC 116, tack 117, seaweedfs 118, dns64
103, and the router 101. The simulated ISPs have no prod counterpart.

Run from [opentofu/](./):

```bash
tofu import \
  'module.suburban.proxmox_virtual_environment_vm.test_mwan' \
  'hypervisor/113'

tofu import \
  'module.suburban.proxmox_virtual_environment_vm.opnsense_test' \
  'hypervisor/101'

tofu import \
  'module.suburban.proxmox_virtual_environment_container.mwan_failover_test' \
  'hypervisor/116'

tofu import \
  'module.suburban.proxmox_virtual_environment_container.tack_qa' \
  'hypervisor/117'

tofu import \
  'module.suburban.proxmox_virtual_environment_container.seaweedfs' \
  'hypervisor/118'

tofu import \
  'module.suburban.proxmox_virtual_environment_container.dns64' \
  'hypervisor/103'

tofu import \
  'module.suburban.proxmox_virtual_environment_container.isp_webpass' \
  'hypervisor/200'

tofu import \
  'module.suburban.proxmox_virtual_environment_container.isp_att' \
  'hypervisor/201'

tofu import \
  'module.suburban.proxmox_virtual_environment_container.isp_mbrains' \
  'hypervisor/202'
```

### Changing a guest's VMID

`vm_id` cannot be updated in place, so a VMID change is a rename on the hypervisor
followed by re-attaching state, never a destroy. Rename the ZFS datasets, which
carries their snapshots, rewrite the volume reference in every `[snapname]` section
of the guest conf as well as the active one, move the conf to the new id, and start
the guest. Then `tofu state rm` the resource and `tofu import` it at the new id.
`tofu state mv` alone is wrong here: it renames the address but leaves the old
`vm_id` in state, which the next plan reads as a replacement.

## Every resource carries prevent_destroy

Each guest, bridge, and VLAN in both modules sets `lifecycle.prevent_destroy = true`,
and a newly imported one must too. These resources are adopted from running
infrastructure, so a destroy is never the intended outcome of a plan.

Understand what the guard covers. It refuses a destroy or a replacement, which is why
an accidental delete or a ForceNew attribute change fails the plan instead of running.
It does nothing about an update in place, so it is not protection against a
destructive attribute change. Read the plan for that.

## Read the plan by kind, not by count

`Plan: N to change` says nothing about whether a change is safe. The provider records
its own defaults on a freshly imported resource, so a plan routinely proposes adding
`timeout_*` values and blocks the import could not populate. Those are bookkeeping.
In the same count can sit an attribute mutation that damages a running guest.

Before any apply, read the attribute diffs and separate the two. Treat any `~` on a
`cpu`, `memory`, or `disk` block as a real hardware change and confirm it is intended.

The dangerous direction is config that understates the live guest. Proxmox cannot
shrink a container or VM disk, so a config declaring less than the hypervisor has
turns into a shrink proposal that either fails or damages the store. That happens
whenever a guest is resized on the hypervisor and the config does not follow, so make
the config change part of the resize rather than a later cleanup.

A plan that has been failing for a while is its own hazard, because drift keeps
accumulating behind the error and none of it is visible. After fixing whatever broke
the plan, read the whole thing again rather than applying the first clean run.

## Drift expectations

- `kvm_arguments` is intentionally absent from the MWAN and OPNsense VM resources.
  Ansible owns the live `args` values because the Proxmox API rejects token
  writes to that field. Both resources list it in `ignore_changes`, because
  undeclared and unignored the provider reads the live value as removed and an
  apply nulls it. On the OPNsense VM that field carries the virtio-serial chardev
  the mwan-opnsense out-of-band daemon serves on. The MWAN VM's args also carry its
  vsock CID, which tracks its VMID.
- `initialization.user_account` is not persisted by the provider, because Proxmox
  never returns injected SSH keys. Resources ignore the whole block, not just
  `keys`: a re-imported guest has no `user_account` at all, so the configured keys
  read as an addition that forces a replacement.
- `operating_system.template_file_id` on imported LXCs is informational because
  Proxmox does not store the original template name in `pct config`.
- `/etc/network/interfaces.d/testbed-masquerade.conf` and the extra routable
  `vmbr1` IPv6 address remain Ansible-owned sourced files.
- A container's state `id` is the bare VMID, so the unique key is the pair of
  `node_name` and `id`. Testbed VMIDs match production's, so the same id appears
  twice across the two hypervisors. Match resources on the pair, never on the id
  or the resource name alone.
