# Prepare guests for service deployment

Run guest preparation before the first MWAN, Tack, proxy, AdGuard, DNS64, or
SeaweedFS deploy on each guest. Existing guests also require a preparation run
before their first service deploy with the revision check.

1. Select the inventory host or group to prepare. Use the same selection for
   `--limit` and `target_hosts`. For the production MWAN guest, run:

   ```bash
   ./configsctl deploy prep-guests --limit mwan_servers --extra-var target_hosts=mwan_servers
   ```

   Preparation installs base packages, updates SSH authorization, configures
   maintenance services, and rotates the guest's SMTP password. Schedule this
   run when SSH and maintenance configuration can change.

2. Confirm that preparation succeeds. A failed run does not record completion.
   A check run does not record completion either.

3. Run the service deploy with the same limit:

   ```bash
   ./configsctl deploy deploy-mwan --limit mwan_servers
   ```

## Apply preparation updates

1. Increase `guest_prep_revision` in the
   [shared variables](../../../ansible/inventory/group_vars/all/vars.yml)
   when a preparation change must apply before service deployment.

2. Repeat preparation for each affected guest before deploying its service.
   Repeat preparation after changing its hostname, SSH authorization, SMTP
   settings, or other base configuration, even when the revision is unchanged.

## Recover a failed preparation run

1. Resolve the failed task and repeat preparation. Preparation removes the
   previous completion record before configuring the guest. Service deployment
   remains blocked until preparation succeeds.

2. If guest SSH access fails, repeat preparation through the hypervisor.
   Preparation installs SSH authorization through Proxmox before connecting
   directly to the guest. A service deploy does not repair SSH access.
