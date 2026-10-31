# Normalize the production router adapters

Use `executing-plans` to complete this plan.

VM 101 must retain `net0` on `vmbr0` with MAC `BC:24:11:AD:E8:92` and
`net1` on `mwanbr` with MAC `BC:24:11:99:20:E4`. Remove the unused `net2`
and `net3` adapters. OpenTofu must manage these consecutive slots.

## Constraints

Keep permissions unchanged. Preserve the serial control channel and the
guest's existing `vtnet0` and `vtnet1` assignments. Run OpenTofu through
configsctl. Take snapshots without saved RAM. Verify production first while
physical and serial access are available.

## Declare the active adapters

- [x] Update the second adapter MAC in
  [the production router module](../../../opentofu/modules/vault/opnsense/main.tf).
- [x] Remove `network_device` from `ignore_changes` and replace the stale
  sparse-layout and state-refresh comments.
- [x] Validate the module and inspect the complete production plan. Require
  retention of `net0` and `net1` and removal of only `net2` and `net3`.

## Apply and verify production

- [x] Record VM 101's hardware, boot identity, interfaces, BGP sessions,
  kernel default routes, and IPv4 and IPv6 connectivity.
- [x] Save its current configuration and a snapshot. Confirm serial access.
- [x] Apply the reviewed plan and compare the live adapters with it.
- [x] Cold restart VM 101 and repeat the baseline checks. Require the same
  active MAC addresses and interface assignments, established BGP sessions,
  kernel defaults, and successful IPv4 and IPv6 internet probes.
- [x] Require an empty subsequent plan, repeat the apply, and compare live
  adapters again.

## Recovery

If a verification fails, inspect the failed boundary before continuing.
Restore the recorded adapter configuration or router snapshot through the
hypervisor and confirm interface assignments before resuming.
