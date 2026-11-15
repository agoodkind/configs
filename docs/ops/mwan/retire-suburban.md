# Retire the Suburban routing testbed

Applying this teardown deletes the MWAN, OPNsense, and simulator guests and their
disks. Removing OPNsense also removes the service LAN's configured gateway at
`10.240.4.1` and `3d06:bad:b01:210::1`. Establish a replacement gateway before
applying this change if surviving services still depend on that gateway.

Run these steps from clean, merged configs source. Retain each encrypted backend
and its provider aliases until its state no longer contains the retired guests.
The `removed` blocks request destruction rather than state removal alone.

## Disable guest recovery and router control

1. Prepare a plan that disables the testbed watchdog and the OPNsense host
   bridge and drain services:

   ```bash
   ./configsctl tofu suburban/mwan plan '-target=pveguest_host_systemd_unit.mwan["mwan-watchdog-testbed.service"]' -target=pveguest_host_systemd_unit.opnsense_bridge -target=pveguest_host_systemd_unit.opnsense_drain -out=/private/tmp/suburban-router-controls.tfplan
   ```

2. Inspect that plan. Require the three services to become inactive and disabled.
   Preserve `mwan-ifmgr.service`, which manages the host's out-of-band interface.
   Apply the reviewed plan before deleting the guests:

   ```bash
   ./configsctl tofu suburban/mwan apply /private/tmp/suburban-router-controls.tfplan
   ```

## Delete the guests through their existing roots

1. Plan and apply the MWAN root to remove gateway VM 213, failover LXC 216,
   and their guest runtime resources:

   ```bash
   ./configsctl tofu suburban/mwan plan -out=/private/tmp/suburban-mwan-teardown.tfplan
   ./configsctl tofu suburban/mwan apply /private/tmp/suburban-mwan-teardown.tfplan
   ```

2. Plan and apply each simulator root separately. These roots own clients 225
   and 226 and simulator LXCs 900 through 920. Use a distinct saved plan for each:

   - `suburban/simulators/lan-clients`
   - `suburban/simulators/astound`
   - `suburban/simulators/astound-static`
   - `suburban/simulators/att`
   - `suburban/simulators/etheric-native`
   - `suburban/simulators/mbrains`
   - `suburban/simulators/routed`
   - `suburban/simulators/router2`
   - `suburban/simulators/sonic`
   - `suburban/simulators/tunnel-static`
   - `suburban/simulators/tunnel-upstream`
   - `suburban/simulators/tunnel-vps`
   - `suburban/simulators/webpass`

   ```bash
   ./configsctl tofu suburban/simulators/lan-clients plan -out=/private/tmp/suburban-clients-teardown.tfplan
   ./configsctl tofu suburban/simulators/lan-clients apply /private/tmp/suburban-clients-teardown.tfplan
   ```

3. Delete OPNsense VM 201 last:

   ```bash
   ./configsctl tofu suburban/opnsense plan -out=/private/tmp/suburban-opnsense-teardown.tfplan
   ./configsctl tofu suburban/opnsense apply /private/tmp/suburban-opnsense-teardown.tfplan
   ```

4. Verify that Proxmox no longer lists guests 201, 213, 216, 225, 226, or 900
   through 920. Verify that their disks no longer exist and that repeated plans
   do not propose creating guests. Preserve shared bridges and unrelated guests.
