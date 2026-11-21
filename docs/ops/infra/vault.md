# Migrate Vault guest networking

Schedule a Tack maintenance window. During the move, private database traffic cannot cross between the old guest bridge and the separate SFP LAN.

## Prepare recovery and management inputs

1. Use a clean, merged checkout for controller commands. Save the previous deployed checkout, encrypted state snapshots, and current guest backups for rollback. On Vault, inventory running and stopped guests with `pct list` and `qm list`. Compare each affected guest's VMID, disks, and current attachment with `pct config "$id"` or `qm config "$id"`.
2. Match the affected guests to their service keys, VMIDs, and VF assignments in the [Vault guest network definitions](../../../opentofu/modules/configuration/hosts/vault/network.tf). Find each owning root in the [Vault workspaces](../../../opentofu/vault). Run `./configsctl providers install`, then `./configsctl tofu "$root" init -input=false` for each affected root, including `vault/host`, `shared/cloudflare-dns`, and `shared/cloudflare-zero-trust`. Obtain the assigned management settings from the management LAN operator. Use the same values for the host, management DNS, and host tunnel routes.

   ```sh
   export TF_VAR_management_ipv4_cidr="${VAULT_MANAGEMENT_IPV4_CIDR:?supply assigned IPv4 address/prefix}"
   export TF_VAR_management_ipv6_cidr="${VAULT_MANAGEMENT_IPV6_CIDR:?supply assigned IPv6 address/prefix}"
   export TF_VAR_management_ipv4_gateway="${VAULT_MANAGEMENT_IPV4_GATEWAY:?supply IPv4 gateway}"
   export TF_VAR_management_ipv6_gateway="${VAULT_MANAGEMENT_IPV6_GATEWAY:?supply IPv6 gateway}"
   ```

3. Inspect each root's `./configsctl tofu "$root" state list` and plan. Record the full guest network module and attachment resource addresses. Use the workspace source and planned addresses for guests absent from state. Some plans depend on the VF pool and PCI mapping. Use the source to identify those addresses first and check the plans after preparing the SFP port. Stage every guest before moving any guest. Move the groups in this order, by ascending VMID within each group.

   1. Guests outside Tack.
   2. Tack application and search containers.
   3. The Tack Kubernetes VM.
   4. Tack database containers, one peer at a time.

4. Set `pf` and `management_if` in the controller and Vault shells. Use the guest PF output above and the [host network resources](../../../opentofu/vault/host/network.tf) for the values. On Vault, use `bridge link` and `ip -br link` to confirm the ports and record the management bridge as `retired_uplink` if it will be removed. Verify that the SFP LAN supplies DHCPv4 and IPv6 router advertisements and permits the planned guest MAC addresses. Compare the supported VF count with the host plan.

   ```sh
   cat "/sys/class/net/${pf:?set the guest PF}/device/sriov_totalvfs"
   dpkg-query -W pve-manager pve-container qemu-server
   ```

   Compare the installed package versions with the verification results for the revision selected by the [Vault overlay call](../../../opentofu/vault/host/main.tf). If the results do not cover the installed versions, verify that revision against those packages in a disposable Proxmox environment before applying it to Vault.

5. Record successful private management, guest, and application checks, including database membership and redundancy. Back up each affected guest's running configuration and disk identities, net0, on-boot setting, and any cloud-init IP configuration. Save network files and permissions outside the guest, including existing files under `/etc/network`, `/etc/netplan`, `/etc/systemd/network`, and `/etc/dhcp`. Record absent directories. In a root shell on Vault, save the host configuration and record the path to the recovery directory outside that SSH session.

   ```sh
   umask 077
   recovery=$(mktemp -d /root/vault-network.XXXXXXXX)
   cp -a /etc/network "$recovery/"
   cp -a /etc/systemd/network "$recovery/networkd"
   cp -a /etc/sysctl.d "$recovery/"
   cp -a /etc/pve/sdn "$recovery/"
   cp -a /etc/pve/nodes/vault/lxc "$recovery/"
   cp -a /etc/pve/nodes/vault/qemu-server "$recovery/"
   nft list ruleset > "$recovery/nftables"
   iptables-save > "$recovery/iptables"
   ip6tables-save > "$recovery/ip6tables"
   ip route show table all > "$recovery/routes4"
   ip -6 route show table all > "$recovery/routes6"
   ```

## Import guests and prepare the SFP port

1. Review the full `vault/host` plan. If it removes the former interfaces file from state, confirm that it leaves the file on disk. Import UniFi if its resource is absent from state. Set `root`, `attachment`, and `id` from the inventory. Import each existing physical interface stanza only if the corresponding resource is absent from state.

   ```sh
   ./configsctl tofu "$root" import "$attachment" "vault/$id"
   ./configsctl tofu vault/host import pveguest_host_network_interface.guest_pf "vault:$pf"
   ./configsctl tofu vault/host import pveguest_host_network_interface.management "vault:$management_if"
   ./configsctl tofu vault/host apply '-target=module.overlay["vault"]'
   ```

   On Vault, run `pve-overlay status` and `pve-overlay compile`. Set `automation_user` from the overlay call and verify the effective guest and SR-IOV privileges with `pveum user permissions "$automation_user"`. If Proxmox already has a saved PF declaration, inspect its assignments and import `pveguest_host_sriov.guests` as `vault:$pf` if it is absent from state.

2. Inspect pending network edits with `pvesh get /nodes/vault/network` and check for `/etc/network/interfaces.new`. Proxmox activates every pending node network edit. Resolve unrelated edits before applying the VF pool. Leave management on its original bridge during this stage.

   ```sh
   ./configsctl tofu vault/host apply -target=pveguest_host_sriov.guests
   ```

3. Compare the live VF count, driver bindings, MACs, and host interface names with the plan. Verify that the PF has no addresses. Set `tack_vf_index` from the Tack Kubernetes VM's guest network definition. Its VF must be the only device in its IOMMU group. Count PCI bridges as group members and stop if the check fails.

   ```sh
   pvesh get "/nodes/vault/sriov/$pf" --output-format json-pretty
   ip address show dev "$pf"
   (
     if ! vf=$(readlink -e "/sys/class/net/${pf:?set the guest PF}/device/virtfn${tack_vf_index:?set the VM VF index}"); then
       printf 'Cannot find VF %s for %s.\n' "$tack_vf_index" "$pf" >&2
       exit 1
     fi
     if ! members=$(ls -A1 "$vf/iommu_group/devices/"); then
       printf 'Cannot list the IOMMU group for VF %s at %s.\n' "$tack_vf_index" "${vf##*/}" >&2
       exit 1
     fi
     printf '%s\n' "$members"
     if [ "$members" != "${vf##*/}" ]; then
       printf 'VF %s at %s must be the only device in its IOMMU group.\n' "$tack_vf_index" "${vf##*/}" >&2
       exit 1
     fi
   )
   ```

4. Apply `./configsctl tofu vault/tack-prod apply -target=proxmox_hardware_mapping_pci.tack` from the controller. Set `pci_mapping` from the [mapping definition](../../../opentofu/vault/tack-prod/pci.tf). Compare `pvesh get "/cluster/mapping/pci/$pci_mapping" --output-format json-pretty` with the VF's PCI address and group.
5. Import the remaining guests absent from state with the guest import command from step 1. Confirm that each state entry references the original guest and disks. Review every plan and stop on guest creation, replacement, disk changes, or changes outside the current stage.

## Stage guest networking and replace attachments

1. Stage each guest while its current network and package repositories work. Set `root` to its owning workspace, `id` to its VMID, `guest` to its full guest network module address, and `attachment` to its full guest resource address. Verify the network backend in the plan. Stage only the guest interface and its package dependencies. Check that the existing default routes still work. Leave forwarding and neighbor discovery active inside guests that use them.

   ```sh
   ./configsctl tofu "$root" apply "-var=staged_guest_ids=[$id]" "-target=$guest.pveguest_network_interface.eth0"
   ```

2. Move one guest at a time in the recorded order. On Vault, run `pct shutdown "$id" --timeout 120` and `pct status "$id"` for a container. For the VM, use `qm shutdown "$id" --timeout 120` and `qm status "$id"`. Wait for the stopped state before changing the attachment.
3. On the controller, set the variables for that guest again. Apply the net0 removal while the guest stays stopped. The first command also attaches the VM's PCI mapping. For a container, run the second command separately to attach its host interface.

   ```sh
   ./configsctl tofu "$root" apply "-var=stopped_guest_ids=[$id]" "-target=$attachment"
   ./configsctl tofu "$root" apply "-var=stopped_guest_ids=[$id]" "-target=$guest.pveguest_container_options.hostnic[0]"
   ```

4. For the stopped VM, inspect its current configuration. If it still has `ipconfig0`, remove that cloud-init setting with the first command. Save the resulting configuration, verify that net0 and ipconfig0 are absent, and compare the disk identities and PCI mapping with the recovery record.

   ```sh
   pvesh set "/nodes/vault/qemu/$id/config" -delete ipconfig0
   pvesh get "/nodes/vault/qemu/$id/config" --current 1 --output-format json > "$recovery/guest-$id-after.json"
   jq -e --arg mapping "${pci_mapping:?set the VM PCI mapping}" 'select((has("ipconfig0") or has("net0")) | not) | select(.hostpci0 | contains($mapping))' "$recovery/guest-$id-after.json"
   ```

5. Inspect `pct config "$id"` or `qm config "$id"`. Confirm the original disks, removal of net0, and the expected hostnic0 or hostpci0. Apply the normal startup settings to boot the guest. Activate eth0 only after the boot loads its staged files. For later changes to a guest using networkd, repeat staging, restart, and activation.

   ```sh
   ./configsctl tofu "$root" apply "-target=$attachment"
   ./configsctl tofu "$root" apply "-target=$guest.pveguest_network_interface.eth0"
   ```

6. In the guest, check `ip -br link show eth0`, `ip address show dev eth0`, `ip route show default`, and `ip -6 route show default`. Require the planned MAC and private addresses, a DHCPv4 lease, a global SLAAC address, and default routes via the SFP LAN. Reject the previous guest gateways and verify resolution of the management name from the host plan. Before removing the old uplink, test UniFi from the SFP LAN using the private A and AAAA records in the DNS plan.
7. After **each** Tack database peer starts, verify its processes and addresses. From the Tack Kubernetes VM, test that peer's configured FoundationDB and YugabyteDB listeners. Record the result of a known ticket read for each move. Check elections and database availability as soon as the migrated peers can form a majority on the SFP LAN. Before declaring recovery, compare database membership and redundancy with the healthy baseline and require a successful ticket read.

   Run the queries from the controller through Vault's Proxmox guest command API. Set `VAULT_API_URL` to the trusted HTTPS Proxmox origin and `VAULT_API_HEADER_FILE` to a private file containing the complete Authorization header. Set `data_id` to the VMID of a data container from the inventory. After defining the function, use `guest_query docker ps` to identify the running containers. Set `fdb_container` and `yb_container` to their names. Get the YugabyteDB master addresses and certificate directory from the active database configuration.

   The function prints its private local directory and saves `request.json`, `start.json`, `status.json`, and `ack.json`. If polling or acknowledgment fails, set `record` to the printed directory and recover the command ID with `jq -er '.data.pid' "$record/start.json"`. Use that ID to resume polling with `exec-status` or to retry `exec-ack` after reading the completed `status.json`. The API retains completed output until acknowledgment or expiry.

   ```bash
   guest_query() (
     set -euo pipefail
     umask 077
     record=$(mktemp -d)
     echo "Guest command records in $record"
     endpoint="${VAULT_API_URL:?supply the trusted Proxmox origin}/api2/json/nodes/vault/lxc/${data_id:?set the data container VMID}"
     api=(curl --fail-with-body --silent --show-error --max-time 15 --header "@${VAULT_API_HEADER_FILE:?supply the token header file}")
     jq -n --args '{"command": $ARGS.positional, "timeout": 30, "result-retention": "acknowledged"}' -- "$@" > "$record/request.json"
     "${api[@]}" --json "@$record/request.json" "$endpoint/exec" > "$record/start.json"
     pid=$(jq -er '.data.pid | numbers | select(. > 0)' "$record/start.json")
     deadline=$((SECONDS + 45))
     while (( SECONDS < deadline )); do
       "${api[@]}" --get --data-urlencode "pid=$pid" "$endpoint/exec-status" > "$record/status.json"
       if jq -e '.data.exited == true or .data.exited == 1' "$record/status.json" >/dev/null; then
         break
       fi
       sleep 1
     done
     jq -e '.data | select(.exited == true or .exited == 1) | select((."out-data" | type) == "string" and (."err-data" | type) == "string") | .exitcode | numbers' "$record/status.json" >/dev/null
     jq -r '.data."out-data" | @base64d' "$record/status.json"
     jq -r '.data."err-data" | @base64d' "$record/status.json" >&2
     "${api[@]}" --data-urlencode "pid=$pid" "$endpoint/exec-ack" > "$record/ack.json"
     jq -e '.data | .exitcode == 0 and ([."timed-out", ."out-truncated", ."err-truncated"] | all(. != true and . != 1))' "$record/status.json" >/dev/null
   )
   guest_query docker exec "${fdb_container:?set the FoundationDB container name}" fdbcli --timeout 10 --exec 'status details'
   guest_query docker exec "${yb_container:?set the YugabyteDB container name}" /home/yugabyte/bin/yb-admin --master_addresses "${yb_masters:?set the configured master addresses}" --certs_dir_name "${yb_certs:?set the client certificate directory}" list_all_masters
   ```

   Check the Tack application rollout with `k3s kubectl rollout status` for the deployment in the current configuration. From the Tack Kubernetes VM, test the configured Kafka listener, Tack search, and the configured SeaweedFS backup endpoint.

## Switch private access and management

1. On the Tack Kubernetes VM, confirm the configured production connector Pods are ready. Enable its private routes and verify the existing Tack HTTP ingress. Provision the management routes and WARP profile entries through the independent Vault tunnel. Check the planned prefixes and connectors before applying.

   ```sh
   ./configsctl tofu shared/cloudflare-zero-trust apply '-target=cloudflare_zero_trust_tunnel_cloudflared_config.tack_k3s["prod"]'
   ./configsctl tofu shared/cloudflare-zero-trust apply -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared_route.vault_management -target=module.cloudflare.cloudflare_zero_trust_device_custom_profile.berylax_lan -target=module.cloudflare.cloudflare_zero_trust_device_custom_profile.tun_only
   ```

2. Establish a direct controller SSH and API connection through the management LAN. Check whether the guest routes cover any old management addresses. If needed, temporarily resolve the management name from the host plan to the working LAN address on the controller. Switch the guest routes, then test private IPv4 and IPv6, UniFi, and guest SSH from a WARP client.

   ```sh
   ./configsctl tofu shared/cloudflare-zero-trust apply -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared_route.home_ipv4 -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared_route.home_guests
   ```

3. Establish a recovery path before changing the active management configuration. Test the [independent console](oob.md), or verify that a timed rollback running on Vault will restore the saved management configuration if SSH drops. If using a timer, confirm it is armed before activation and cancel it only after management connectivity passes the checks below.
4. On Vault, verify UniFi uses its VF and responds at its private service addresses. Use the active interface includes and `networkctl status "$retired_uplink"` to identify the old bridge definitions. Move only those definitions to the recovery directory. After cutover, disable systemd-networkd on Vault only if it manages no remaining links.
5. From the controller, stage static management.

   ```sh
   ./configsctl tofu vault/host apply -target=pveguest_host_network_interface.management
   ```

   On Vault, inspect the pending interface configuration. Require the assigned addresses and gateways directly on the management interface, without bridge membership. Activate only the reviewed configuration.

   ```sh
   ./configsctl tofu vault/host apply -target=proxmox_network_applier.management
   ```

   If the connection drops, use the recovery path from step 3. With console access, inspect the running addresses and pending configuration. Use `pvesh set /nodes/vault/network` to complete the reviewed activation. If the retired bridge still owns the port, use `ip link set "$management_if" nomaster`, remove the unused bridge with `ip link delete "$retired_uplink" type bridge`, then run `ifreload -a`. Verify the two addresses, both gateways, and the host tunnel.

6. Review `./configsctl tofu shared/cloudflare-dns plan`. Set `dns_a_target` and `dns_aaaa_target` to the full resource addresses for the planned management A and AAAA records. Confirm that their values match the assigned addresses.

   ```sh
   ./configsctl tofu shared/cloudflare-dns apply "-target=${dns_a_target:?set the management A record resource address}" "-target=${dns_aaaa_target:?set the management AAAA record resource address}"
   ./configsctl tofu vault/host apply -target=linux_systemd_unit.cloudflared
   ```

   Remove temporary name overrides. From a WARP client, test SSH and the Proxmox API over IPv4 and IPv6 using the management name. Confirm the management routes use the host connector and the guest routes use the Tack connector.

## Retire the bridges and verify reboot

1. On Vault, check `bridge link` and the live guest configurations. Confirm that the bridges to retire have no guest attachments. Use `pvesh get /cluster/sdn/vnets` and `pvesh get /cluster/sdn/zones` to identify the old VNet and zone. Set `retired_vnet` and `retired_zone` from that output and verify that the zone contains no other VNets. List the old subnets. Run the subnet deletion once for each exact ID before deleting the VNet and zone. Review pending changes before the final SDN apply.

   ```sh
   pvesh get "/cluster/sdn/vnets/${retired_vnet:?set the retired VNet}/subnets" --output-format json-pretty
   pvesh delete "/cluster/sdn/vnets/${retired_vnet:?set the retired VNet}/subnets/${retired_subnet:?set the listed subnet ID}"
   pvesh delete "/cluster/sdn/vnets/${retired_vnet:?set the retired VNet}"
   pvesh delete "/cluster/sdn/zones/${retired_zone:?set the retired zone}"
   pvesh set /cluster/sdn
   ```

2. Identify the retired guest network unit and sysctl settings from the previous deployed checkout and live configuration. If still installed, disable the unit and move it and its settings to the recovery directory. Inspect `nft list ruleset`, `iptables-save -t nat`, and `ip6tables-save -t nat`. Remove only translation rules or dedicated tables for the retired guest network. From the controller, run `./configsctl tofu vault/host apply -target=linux_sysctl.forwarding`.
3. Check `bridge link`, host addresses and routes, and both host forwarding settings. Require an addressless SFP PF, direct static management on the assigned interface, disabled host forwarding, and removal of the old gateways and translation. Review ordinary plans for every affected workspace. Apply the state-only retirements and resolve unexpected changes before reboot.
4. Verify a recovery path that works if Vault does not regain management connectivity after reboot. Reboot during the maintenance window. Check the VF restore, Proxmox guest startup, and host tunnel units with `systemctl status pve-overlay-sriov.service pve-guests.service cloudflared.service`. Recheck VF assignments, guest autostart, disks, automatic addresses, private host and guest access, and the Tack database and application checks. Retain the recovery copies until those checks pass.

## Roll back an interrupted move

1. Stop migration applies. Keep the maintenance window open until the Tack application and its database majority use the same Ethernet network.
2. If host management fails, use the recovery path established before activation. Restore the saved interface files, networkd configuration, and forwarding settings. Restore the saved SDN definitions if necessary. Review pending changes and apply them with `pvesh set /cluster/sdn`. Enable the original network services, run `ifreload -a`, and restore only the retired translation rules from the saved firewall configuration. Confirm the original management and host connector access.
3. Restore each affected guest's saved network files and permissions, including the original network manager configuration and DHCP hooks. Remove the per-interface files created during staging. Shut down the guest and remove its VF attachment with `pct set "$id" -delete hostnic0` or `qm set "$id" -delete hostpci0`. Restore its exact saved net0 and on-boot setting. For example, use `pct set "$id" -net0 "$original_net0"` for a container. Restore the VM's saved cloud-init IP configuration. Start the guest and verify its original addresses, routes, and disks.
4. Restore the old guest tunnel routes and management DNS records through the previous deployed configuration. Confirm host and guest access from WARP. If reverting the overlay, release every VF attachment and remove the saved VF declaration through the node API first. Review a fresh plan against the restored configuration before resuming ordinary applies.
