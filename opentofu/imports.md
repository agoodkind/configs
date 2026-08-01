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
2. `tofu init` has run against the local backend; the state file lives at
   `opentofu/terraform.tfstate` on the operator workstation and is gitignored.
3. The live suburban shape matches the target resources before import:

```bash
ssh suburban 'pvesh get /nodes/hypervisor/network --output-format json'
ssh suburban 'qm list; pct list'
```

Compare that listing against
[service_mapping.yml](../ansible/inventory/group_vars/all/service_mapping.yml),
then read the config of each guest you are about to import with `qm config` or
`pct config`. [testbed/pve-configs.txt](../testbed/pve-configs.txt) holds a
capture of the same output if you want a diff target.

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

No testbed VMID equals a production one. A guest that mirrors a production
service takes its counterpart's id plus 100, and the simulated ISPs, which have
no production counterpart, sit in 9xx.

The separation is what keeps a misdirected command safe. The two hypervisors are
independent installations rather than a cluster, so a shared id is legal, but a
command that lands on the wrong host then finds a guest there and succeeds
against it. With no id in common, the same mistake fails with "no such guest".

Import each guest at the id
[service_mapping.yml](../ansible/inventory/group_vars/all/service_mapping.yml)
gives it. The script below reads those ids rather than repeating them, so it
stays correct through a renumber. Run it from [opentofu/](./):

```bash
python3 - <<'PY' | sh
import yaml
mapping = yaml.safe_load(
    open("../ansible/inventory/group_vars/all/service_mapping.yml")
)["service_mapping"]
# OpenTofu resource name -> service_mapping key
guests = {
    "proxmox_virtual_environment_vm.opnsense_test": "opnsense_test",
    "proxmox_virtual_environment_vm.test_mwan": "test_mwan",
    "proxmox_virtual_environment_container.mwan_failover_test": "mwan_failover_test",
    "proxmox_virtual_environment_container.dns64": "dns64_suburban",
    "proxmox_virtual_environment_container.tack_qa": "tack_qa",
    "proxmox_virtual_environment_container.seaweedfs": "seaweedfs_suburban",
    "proxmox_virtual_environment_container.isp_webpass": "isp_webpass",
    "proxmox_virtual_environment_container.isp_att": "isp_att",
    "proxmox_virtual_environment_container.isp_mbrains": "isp_mbrains",
}
for resource, service in guests.items():
    print(f"tofu import 'module.suburban.{resource}' 'hypervisor/{mapping[service]['vmid']}'")
PY
```

Drop the `| sh` to read the commands before running them.

### Change a guest's VMID

Proxmox treats the VMID as the guest's identity and `vm_id` cannot be updated in
place, so a VMID change is a rename on the hypervisor plus a state re-attach. It is
never a destroy.

Renaming the dataset carries its snapshots, because they are children of it.

1. Stop the guest.
2. Rename each ZFS dataset from the old id to the new one.
3. Rewrite the volume reference in the guest conf. It appears once in the active
   config and once in every `[snapname]` section, so rewrite all of them.
4. Move the conf to the new id and start the guest.
5. Run `tofu state rm` on the resource, then `tofu import` it at the new id.

Do not use `tofu state mv` for this. It renames the address and leaves the old
`vm_id` in state, which the next plan reads as a replacement.

## Keep prevent_destroy on every resource

Each guest, bridge, and VLAN in both modules sets `lifecycle.prevent_destroy = true`,
and a newly imported one needs it too. These resources are adopted from running
infrastructure, so a destroy is never the intended outcome of a plan.

The guard refuses a destroy or a replacement. An accidental delete, or a change to an
attribute that forces replacement, fails the plan instead of running. The guard does
nothing about an update in place, so it is not protection against a destructive
attribute change.

## Read a plan by kind, not by count

`Plan: N to change` says nothing about whether a change is safe.

A freshly imported resource makes the provider propose its own defaults, so a plan
routinely adds `timeout_*` values and blocks that the import could not populate. That
is bookkeeping. The same count can also hold an attribute change that damages a
running guest.

Read the attribute diffs before any apply and separate the two. Treat a `~` on a
`cpu`, `memory`, or `disk` block as a real hardware change and confirm it is intended.

The dangerous direction is config that understates the live guest. Proxmox cannot
shrink a container or VM disk, so config declaring less than the hypervisor has
becomes a shrink proposal, which either fails or damages the guest's data. Resizing a
guest on the hypervisor without updating the config is what produces that state, so
change the config as part of the resize.

A plan that has been failing for a while is its own hazard, because drift accumulates
behind the error where nobody sees it. After fixing whatever broke the plan, read the
whole plan again rather than applying the first clean run.

## Drift expectations

- Ansible owns the live `args` on the MWAN and OPNsense VMs, because the Proxmox API
  rejects token writes to that field. Neither resource declares `kvm_arguments`, and
  both name it in `ignore_changes`. Without the ignore, the provider reads the live
  value as removed and an apply nulls it. On the OPNsense VM that field carries the
  virtio-serial chardev the mwan-opnsense out-of-band daemon serves on. The MWAN VM's
  args carry its vsock CID, which tracks its VMID.
- The provider does not persist `initialization.user_account`, because Proxmox never
  returns injected SSH keys. Resources ignore the whole block rather than `keys`
  alone. A re-imported guest has no `user_account` at all, so the configured keys
  read as an addition, and that forces a replacement.
- `operating_system.template_file_id` on imported LXCs is informational because
  Proxmox does not store the original template name in `pct config`.
- `/etc/network/interfaces.d/testbed-masquerade.conf` and the extra routable
  `vmbr1` IPv6 address remain Ansible-owned sourced files.
- A container's state `id` is the bare VMID, so the unique key is the pair of
  `node_name` and `id`. No id is shared across the two hypervisors today, but
  match resources on the pair anyway, because the id alone carries no hypervisor
  and a future guest could reintroduce an overlap.
