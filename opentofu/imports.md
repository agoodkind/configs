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
ssh suburban 'qm config 950'
ssh suburban 'qm config 101'
ssh suburban 'pct config 100'
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

```bash
tofu import \
  'module.suburban.proxmox_virtual_environment_vm.vm950_test_mwan' \
  'hypervisor/950'

tofu import \
  'module.suburban.proxmox_virtual_environment_vm.opnsense_test' \
  'hypervisor/101'

tofu import \
  'module.suburban.proxmox_virtual_environment_container.mwan_failover_test' \
  'hypervisor/100'

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

If `tack-qa` already exists live and is not yet in state, import it with:

```bash
tofu import \
  'module.suburban.proxmox_virtual_environment_container.tack_qa' \
  'hypervisor/103'
```

## Drift expectations

- `kvm_arguments` is intentionally absent from VM 950 and VM 101 resources.
  Ansible owns the live `args` values because the Proxmox API rejects token
  writes to that field.
- `initialization.user_account.keys` can change when GitHub public keys rotate.
  Resources ignore that field where it would otherwise create noise.
- `operating_system.template_file_id` on imported LXCs is informational because
  Proxmox does not store the original template name in `pct config`.
- `/etc/network/interfaces.d/testbed-masquerade.conf` and the extra routable
  `vmbr1` IPv6 address remain Ansible-owned sourced files.
