# OpenTofu import recipe for suburban testbed resources

This file documents how an operator brings the live suburban resources
under OpenTofu control without recreating them. The resources already
exist on suburban (`hypervisor.suburban.goodkind.io`); the goal is to
attach Tofu state to those running objects.

The bpg/proxmox provider import IDs follow these formats:

* `proxmox_virtual_environment_network_linux_bridge`: `<node_name>:<iface>`
  (documented at
  https://registry.terraform.io/providers/bpg/proxmox/latest/docs/resources/virtual_environment_network_linux_bridge)
* `proxmox_virtual_environment_vm`: `<node_name>/<vm_id>`
  (documented at
  https://registry.terraform.io/providers/bpg/proxmox/latest/docs/resources/virtual_environment_vm)

The suburban node name is `hypervisor`.

## Prerequisites

1. `terraform.tfvars` populated with both Proxmox API tokens
   (`proxmox_api_token` for vault, `suburban_proxmox_api_token` for
   suburban). The example file at `terraform.tfvars.example` lists the
   fields. Real token values come from the Ansible vault, never the repo.
2. `tofu init` against the Consul backend at `[3d06:bad:b01::106]:8500`.
   Operators only run init the first time after the suburban provider
   alias is added to `providers.tf`; subsequent runs reuse the cached
   plugins.
3. Live verification of the resources before import. The expected shape
   on suburban as of 2026-05-07:

   ```
   ssh suburban 'qm config 950 | grep -E "args|net|cores|memory|machine"'
   ssh suburban 'ip -br link | grep vmbr-'
   ```

   If `vmbr-trunk` and `vmbr-mgmt` are missing on the live host, the
   mwan-140 slice 1 work has not been applied surgically yet. Apply it on
   suburban first, then import.

## Import commands

Run from `opentofu/` in the worktree (or from repo root after merge):

```bash
# MWAN-63: bridges.
tofu import \
  'proxmox_virtual_environment_network_linux_bridge.trunk' \
  'hypervisor:vmbr-trunk'

tofu import \
  'proxmox_virtual_environment_network_linux_bridge.mgmt' \
  'hypervisor:vmbr-mgmt'

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
* `kvm_arguments`: the bpg/proxmox provider passes the value through to
  the API `args` field unchanged. The live value reads
  `-device vhost-vsock-pci,guest-cid=950`; the resource declares the
  same string.
* `vids`: the bridge resource declares the VLAN list space-separated
  (`64 100 200 300`). If the live config stores it comma-separated, plan
  flags drift; switch the value to `64,100,200,300` and re-plan.

If plan reports unexpected destroy actions on any imported resource,
stop and inspect. The `lifecycle.prevent_destroy = true` blocks will
abort `tofu apply` before damage; treat any such plan as a sign the
resource definition does not match the live shape and tune the HCL
before re-running.

## Out of scope

The suburban testbed includes more resources that this slice does NOT
import:

* LXC 200, 201, 202, 203 (simulated ISP and OPNsense LAN containers).
* VM 101 (testbed OPNsense).
* SDN config and `/etc/network/interfaces.d/testbed-masquerade.conf`.

Those land in a follow-up MWAN-62 slice.
