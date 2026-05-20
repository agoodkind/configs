# OpenTofu import recipe for suburban testbed resources

This file documents how an operator brings the live suburban resources
under OpenTofu control without recreating them. The resources already
exist on suburban (`hypervisor.suburban.goodkind.io`); the goal is to
attach Tofu state to those running objects.

The bpg/proxmox provider import IDs follow these formats:

* `proxmox_virtual_environment_network_linux_bridge`: `<node_name>:<iface>`
  (documented at
  [Terraform Registry: virtual_environment_network_linux_bridge](https://registry.terraform.io/providers/bpg/proxmox/latest/docs/resources/virtual_environment_network_linux_bridge))
* `proxmox_virtual_environment_vm`: `<node_name>/<vm_id>`
  (documented at
  [Terraform Registry: virtual_environment_vm](https://registry.terraform.io/providers/bpg/proxmox/latest/docs/resources/virtual_environment_vm))

The suburban node name is `hypervisor`.

## Prerequisites

1. `terraform.tfvars` populated with both Proxmox API tokens
   (`proxmox_api_token` for vault, `suburban_proxmox_api_token` for
   suburban). The example file at [opentofu/terraform.tfvars.example](./terraform.tfvars.example) lists the
   fields. Real token values come from the Ansible vault, never the repo.
2. `tofu init` against the Consul backend at `[3d06:bad:b01::106]:8500`.
   Operators only run init the first time after the suburban provider
   alias is added to [opentofu/providers.tf](./providers.tf); subsequent runs reuse the cached
   plugins.
3. Live verification of the resources before import. The expected shape
   on suburban as of 2026-05-07:

   ```bash
   ssh suburban 'qm config 950 | grep -E "args|net|cores|memory|machine"'
   ssh suburban 'ip -br link | grep vmbrtrunk'
   ```

   If `vmbrtrunk` is missing on the live host, the mwan-140 slice 1 work
   has not been applied surgically yet. Apply it on suburban first, then
   import. MWAN-148 dropped the separate `vmbrmgmt` bridge from this
   plan, since prod runs MANAGEMENT untagged on the same physical port
   that carries the VLAN trunk.

## Import commands

Run from [opentofu/](./) in the worktree (or from repo root after merge):

```bash
# MWAN-63: trunk bridge.
tofu import \
  'proxmox_network_linux_bridge.trunk' \
  'hypervisor:vmbrtrunk'

# MWAN-62 (partial): VM 950.
tofu import \
  'proxmox_virtual_environment_vm.vm950_test_mwan' \
  'hypervisor/950'
```

After all three imports succeed, run `tofu plan` to confirm the resource
definitions match the live shape. Drift is expected on a few fields; the
common ones are:

* `initialization.user_account.keys`: the GitHub SSH key list rotates
  whenever the operator adds or removes a public key. The resource
  ignores changes on this attribute, so plan should not flag it.
* `kvm_arguments` was removed from the VM 950 resource under MWAN-154.
  Ansible now owns the live `args` value, which reads
  `-device vhost-vsock-pci,guest-cid=950`. `tofu plan` no longer compares
  this field, so the live value is left alone.
* `vids`: the bridge resource declares the VLAN list space-separated
  (`64 100 200 300`). If the live config stores it comma-separated, plan
  flags drift; switch the value to `64,100,200,300` and re-plan.

If plan reports unexpected destroy actions on any imported resource,
stop and inspect. The `lifecycle.prevent_destroy = true` blocks will
abort `tofu apply` before damage; treat any such plan as a sign the
resource definition does not match the live shape and tune the HCL
before re-running.

## Testbed import (MWAN-62)

The MWAN-62 expansion adds the four ISP simulator LXCs, the
mwan-failover-test LXC, and the testbed OPNsense VM. The bpg/proxmox
provider import IDs follow the same `<node_name>/<vm_id>` shape for both
containers and VMs (the container resource accepts the same separator the
VM resource uses).

Run from [opentofu/](./) in the worktree (or from repo root after merge):

```bash
# MWAN-62: suburban testbed LXCs.
tofu import \
  'proxmox_virtual_environment_container.mwan_failover_test' \
  'hypervisor/100'

tofu import \
  'proxmox_virtual_environment_container.isp_webpass' \
  'hypervisor/200'

tofu import \
  'proxmox_virtual_environment_container.isp_att' \
  'hypervisor/201'

tofu import \
  'proxmox_virtual_environment_container.isp_mbrains' \
  'hypervisor/202'

tofu import \
  'proxmox_virtual_environment_container.testbed_proxy' \
  'hypervisor/203'

# MWAN-62: suburban testbed OPNsense VM 101 (opnsense-test).
# This holds the working OPNsense testbed install on `vmbrtrunk` and
# `vmbr2` with the chardev `args` block owned by Ansible.
tofu import \
  'proxmox_virtual_environment_vm.opnsense_test' \
  'hypervisor/101'
```

Drift expectations on `tofu plan` after these imports:

* `operating_system.template_file_id` on every imported LXC. Proxmox does
  not store the original template name in `pct config`, so the value
  declared here is informational. Each LXC resource lists this field in
  `lifecycle.ignore_changes` so plan does not flag it.
* `initialization.ip_config` on the LXCs. The bpg provider models the
  Proxmox-native `ip=`/`ip6=` fields on each net line via this block. The
  values declared here mirror the live `pct config` output as of
  2026-05-07. If plan flags drift, compare against the live config and
  tune the HCL rather than ignoring the field.

## Out of scope

The suburban testbed still includes resources that this slice does NOT
import:

* SDN config and `/etc/network/interfaces.d/testbed-masquerade.conf`.

Those land in a follow-up slice tied to MWAN-140.
