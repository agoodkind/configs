# Migrate Vault guest networking

Schedule a Tack maintenance window. The old guest bridge and the SFP LAN use separate Ethernet networks. Database peers on opposite sides cannot communicate at their private addresses during the move. Open the [independent console](oob.md) before changing production.

## Prepare recovery and management inputs

1. Use a clean, merged checkout for the controller commands. Keep the previous deployed checkout, encrypted state snapshots, and current guest backups for rollback. Run `./configsctl providers install`, then `./configsctl tofu "$root" init -input=false` for each workspace in the table below and for `shared/cloudflare-dns` and `shared/cloudflare-zero-trust`.
2. Export the assigned management settings. Use the same addresses for the host, management DNS, and host tunnel routes. Obtain the values from the management LAN operator. Do not derive them from the current lease.

   ```sh
   export TF_VAR_management_ipv4_cidr="${VAULT_MANAGEMENT_IPV4_CIDR:?supply assigned IPv4 address/prefix}"
   export TF_VAR_management_ipv6_cidr="${VAULT_MANAGEMENT_IPV6_CIDR:?supply assigned IPv6 address/prefix}"
   export TF_VAR_management_ipv4_gateway="${VAULT_MANAGEMENT_IPV4_GATEWAY:?supply IPv4 gateway}"
   export TF_VAR_management_ipv6_gateway="${VAULT_MANAGEMENT_IPV6_GATEWAY:?supply IPv6 gateway}"
   ```

3. On Vault, confirm the physical connections with `bridge link` and `ip -br link`. The existing uplink bridge must use `enp4s0`. Check that the distinct SFP LAN supplies DHCPv4 and IPv6 router advertisements and permits the guest MAC addresses. Verify that `cat /sys/class/net/enp2s0f0np0/device/sriov_totalvfs` reports at least 16. Compare `dpkg-query -W pve-manager pve-container qemu-server` with the tested versions 9.2.28, 6.1.20, and 9.2.15 respectively.
4. Record successful private management, guest, and application checks. In a root shell on Vault, save the host files and guest definitions. Keep this shell and the path to its private recovery directory available on the console.

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

5. Back up each guest's current network files and permissions outside the guest. Include each existing directory under `/etc/network`, `/etc/netplan`, `/etc/systemd/network`, and `/etc/dhcp`. Record which directories were absent. Save the running config, each disk identity, the guest's net0 property string, on-boot setting, and the VM's cloud-init IP configuration. Confirm LXC 102 uses `local-lvm:vm-102-disk-0` and the uplink bridge.

## Import guests and prepare the SFP port

1. Run `./configsctl tofu "$root" state list` for every guest workspace. Import LXC 102 in step 2. For each other guest missing from its owning state, run `./configsctl tofu "$root" import "$attachment" "vault/$id"` with its address below. The container prefix is `module.vault.proxmox_virtual_environment_container.` except for LXC 102. Review every plan. Stop on guest creation, replacement, disk changes, or changes outside that stage.

   | ID | Workspace | Service key | Container suffix or complete resource address |
   | --- | --- | --- | --- |
   | 100 | vault/debianct | debianct | debianct |
   | 102 | vault/host | unifi | proxmox_virtual_environment_container.unifi |
   | 110 | vault/proxy | proxy | proxy |
   | 118 | vault/seaweedfs | seaweedfs | seaweedfs |
   | 117 | vault/tack-prod | tack | tack |
   | 123 | vault/tack-prod | tack_app2 | tack_app2 |
   | 125 | vault/tack-prod | tack_search1 | tack_search1 |
   | 124 | vault/tack-prod | tack_k3s | module.tack_k3s_vm.proxmox_virtual_environment_vm.server |
   | 120 | vault/tack-prod | tack_data1 | tack_data1 |
   | 121 | vault/tack-prod | tack_data2 | tack_data2 |
   | 122 | vault/tack-prod | tack_data3 | tack_data3 |

2. Import the existing physical interface stanzas. Apply the reviewed overlay to enable the guest and SR-IOV APIs. The plan must release the old interfaces file from state without deleting it.

   ```sh
   ./configsctl tofu vault/host import proxmox_virtual_environment_container.unifi vault/102
   ./configsctl tofu vault/host import pveguest_host_network_interface.guest_pf vault:enp2s0f0np0
   ./configsctl tofu vault/host import pveguest_host_network_interface.management vault:enp4s0
   ./configsctl tofu vault/host apply '-target=module.overlay["vault"]'
   ```

   Skip imports already present in state. On Vault, verify `pve-overlay status`, `pve-overlay compile`, and the effective privileges with `pveum user permissions ansible@pam`. If the PF already has a saved declaration, inspect its assignments and import `pveguest_host_sriov.guests` as `vault:enp2s0f0np0`.

3. Inspect pending edits with `pvesh get /nodes/vault/network` and check for `/etc/network/interfaces.new`. Proxmox activates every pending node network edit. Resolve unrelated pending changes before activating the SFP port. Keep management on its original bridge. Apply the VF pool target alone, then the VM's PCI mapping.

   ```sh
   ./configsctl tofu vault/host apply -target=pveguest_host_sriov.guests
   ./configsctl tofu vault/tack-prod apply -target=proxmox_hardware_mapping_pci.tack
   ```

4. On Vault, verify the hardware before changing guests. Require an addressless PF, 16 VFs, VF 0 bound to vfio-pci, and indices 1 through 10 bound to iavf with the assigned MACs and host interface names. Compare the mapping's PCI address and IOMMU group with the VF inventory. Indices 11 through 15 remain available.

   ```sh
   pvesh get /nodes/vault/sriov/enp2s0f0np0 --output-format json-pretty
   pvesh get /cluster/mapping/pci/vault-tack-k3s --output-format json-pretty
   ip address show dev enp2s0f0np0
   ```

## Stage guest networking and replace attachments

1. Stage all guests while their current interfaces and package repositories work. Set the shell variables for each row from the table. This example selects data LXC 120. Target only the interface resource to install its networking packages and edit its persistent network configuration.

   ```sh
   root=vault/tack-prod
   id=120
   guest='module.guest_network["tack_data1"]'
   attachment=module.vault.proxmox_virtual_environment_container.tack_data1
   ./configsctl tofu "$root" apply "-var=staged_guest_ids=[$id]" "-target=$guest.pveguest_network_interface.eth0"
   ```

   Check the current default routes after staging. LXC 110 uses systemd-networkd. VM 124 uses netplan. The other guests use ifupdown with the ISC DHCP client and resolvconf. Leave Docker forwarding and neighbor discovery active inside the guests.

2. Move one guest at a time in table order. On Vault, run `pct shutdown "$id" --timeout 120` and `pct status "$id"`. For VM 124 use `qm shutdown 124 --timeout 120` and `qm status 124`. Wait for the stopped state.
3. On the controller, select that guest's variables again. Apply its old net0 removal while the guest stays stopped. For a container, install hostnic0 in a separate targeted apply. For the VM, the first command also removes its cloud-init gateways and installs hostpci0. Skip the second command for the VM.

   ```sh
   ./configsctl tofu "$root" apply "-var=stopped_guest_ids=[$id]" "-target=$attachment"
   ./configsctl tofu "$root" apply "-var=stopped_guest_ids=[$id]" "-target=$guest.pveguest_container_options.hostnic[0]"
   ```

4. Inspect `pct config "$id"` or `qm config 124` on Vault. Confirm the original disks, removal of net0, and the selected hostnic0 or hostpci0. Use the normal startup settings to start that guest. Activate eth0 only after the controlled boot loads its staged files. For later changes to either networkd backend, repeat the stage, restart, and active sequence.

   ```sh
   ./configsctl tofu "$root" apply "-target=$attachment"
   ./configsctl tofu "$root" apply "-target=$guest.pveguest_network_interface.eth0"
   ```

5. In the guest, check `ip -br link show eth0`, `ip address show dev eth0`, `ip route show default`, `ip -6 route show default`, and `getent hosts hypervisor.home.goodkind.io`. Require the assigned MAC, a DHCPv4 lease, a global SLAAC address, all assigned private addresses, and upstream default routes. Reject either old Vault gateway. Test UniFi through its private A and AAAA records from the SFP LAN before removing the old uplink.
6. Run these database checks after **each** data LXC starts. FoundationDB uses double redundancy and three coordinators. YugabyteDB uses three replicas. The first migrated peer cannot form a majority alone. Verify its processes, addresses, and individual listener connections from VM 124 before continuing. After the second move, check election and database availability again. After the third move, require three live YugabyteDB masters, full FoundationDB replication and one-machine fault tolerance, and a successful Tack read.

   ```sh
   pct exec 120 -- docker exec tack-fdb-1 fdbcli --timeout 10 --exec 'status details'
   pct exec 120 -- docker exec tack-yugabyte-1 /home/yugabyte/bin/yb-admin --master_addresses '[3d06:bad:b01::120]:7100,[3d06:bad:b01::121]:7100,[3d06:bad:b01::122]:7100' --certs_dir_name /home/yugabyte/certs list_all_masters
   ```

   From VM 124, test the FoundationDB and YugabyteDB listeners of the peer just moved. Read a known ticket through the normal Tack client and record the result for each move. Following recovery, run `k3s kubectl -n tack rollout status deployment/tack-app` on VM 124, test its connection to Kafka at `[3d06:bad:b01:0:7ac::4]:29092`, and verify Tack search and the existing SeaweedFS backup endpoint.

## Switch private access and management

1. Confirm both production connector Pods are ready on VM 124. Enable private routing and verify the existing Tack HTTP ingress. Provision the specific management routes and WARP profile entries through the independent Vault tunnel.

   ```sh
   ./configsctl tofu shared/cloudflare-zero-trust apply '-target=cloudflare_zero_trust_tunnel_cloudflared_config.tack_k3s["prod"]'
   ./configsctl tofu shared/cloudflare-zero-trust apply -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared_route.vault_management -target=module.cloudflare.cloudflare_zero_trust_device_custom_profile.berylax_lan -target=module.cloudflare.cloudflare_zero_trust_device_custom_profile.tun_only
   ```

2. Establish a direct controller SSH and API connection through the existing management LAN. The broad guest routes currently include the old management addresses. If necessary, temporarily resolve `hypervisor.home.goodkind.io` on the controller to the working management LAN address. Keep the independent console open. Switch the guest routes, then test private IPv4 and IPv6, UniFi, and guest SSH from a WARP client.

   ```sh
   ./configsctl tofu shared/cloudflare-zero-trust apply -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared_route.home_ipv4 -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared_route.home_guests
   ```

3. On the Vault console, verify LXC 102 uses its VF and responds at its private service addresses. Move the old bridge include and networkd definition outside their active directories. If networkd manages only the retiring uplink, disable its host service after cutover. Preserve other managed links.

   ```sh
   mv /etc/network/interfaces.d/uplink "$recovery/uplink.retired"
   mv /etc/systemd/network/20-vault-uplink.network "$recovery/uplink.network.retired"
   ```

4. On the controller, stage static management. On the console, inspect the pending interfaces file. Require the assigned addresses and gateways directly on enp4s0 and confirm that the new configuration does not enslave enp4s0 to another interface. Activate only this reviewed configuration.

   ```sh
   ./configsctl tofu vault/host apply -target=pveguest_host_network_interface.management
   ./configsctl tofu vault/host apply -target=proxmox_network_applier.management
   ```

   If the old management connection drops during activation, inspect the running addresses and pending configuration on the console. Use `pvesh set /nodes/vault/network` to complete a pending, reviewed activation. If the obsolete bridge still owns the port, detach it on the console with `ip link set enp4s0 nomaster`, remove the unused bridge with `ip link delete uplink type bridge`, and run `ifreload -a`. Verify direct addresses, both gateways, and the host tunnel before continuing.

5. Update management DNS. Remove temporary controller name overrides. From a WARP client, check IPv4 and IPv6 SSH and port 8006 by the management name. Confirm the specific routes use the host connector and the two guest prefixes use Tack.

   ```sh
   ./configsctl tofu shared/cloudflare-dns apply '-target=cloudflare_dns_record.static["goodkind.io/A/hypervisor.home.goodkind.io/9f586dc5"]' '-target=cloudflare_dns_record.static["goodkind.io/AAAA/hypervisor.home.goodkind.io/ec2ffb07"]'
   ./configsctl tofu vault/host apply -target=linux_systemd_unit.cloudflared
   ```

## Retire the bridges and verify reboot

1. On Vault, list the old subnets and use their exact IDs for deletion. Confirm every guest has released both old bridges and the Vault zone does not contain other VNets. Review pending SDN changes before the final apply.

   ```sh
   pvesh get /cluster/sdn/vnets/vmnet/subnets --output-format json-pretty
   pvesh delete "/cluster/sdn/vnets/vmnet/subnets/${OLD_IPV4_SUBNET_ID:?supply the listed IPv4 subnet ID}"
   pvesh delete "/cluster/sdn/vnets/vmnet/subnets/${OLD_IPV6_SUBNET_ID:?supply the listed IPv6 subnet ID}"
   pvesh delete /cluster/sdn/vnets/vmnet
   pvesh delete /cluster/sdn/zones/vault
   pvesh set /cluster/sdn
   ```

2. If installed, disable the obsolete `vault-guest-network.service` and move its unit and configuration into the recovery directory. Move `/etc/sysctl.d/70-vault-guest-network.conf` there. Review `nft list tables`, `iptables-save -t nat`, and `ip6tables-save -t nat`. Remove remaining rules for guest translation only. If the dedicated old tables exist, remove them with `nft destroy table ip vault_guests` and `nft destroy table inet vault_guests`. On the controller, run `./configsctl tofu vault/host apply -target=linux_sysctl.forwarding`.
3. Check `bridge link`, host addresses and routes, and both host forwarding settings. Require an addressless SFP PF, static management directly on enp4s0, disabled host forwarding, and removal of the old gateways and translation. Review ordinary plans for every affected workspace. Apply the state-only retirement records and resolve any unexpected changes before reboot.
4. Reboot Vault during the window. Verify `systemctl status pve-overlay-sriov.service pve-guests.service cloudflared.service`, VF assignments, guest autostart, disks, automatic addresses, private host access, private guest access, and the Tack database and application checks. Keep recovery copies until all checks pass.

## Roll back an interrupted move

1. Stop migration applies. Keep the maintenance window open until the Tack application and its database majority use the same Ethernet network.
2. If host management fails, use the physical console. Restore the saved interface files, networkd unit, and forwarding settings. Restore the saved SDN definitions if necessary. Inspect the pending changes and apply them with `pvesh set /cluster/sdn`. Enable the original network services, run `ifreload -a`, and restore only the retired translation rules from the saved firewall configuration. Confirm original management and host connector access.
3. Restore each affected guest's saved network files, including the original manager configuration and DHCP hooks. Remove the provider's additional per-interface files created during staging. Shut down the guest. Remove its VF attachment with `pct set "$id" -delete hostnic0` or `qm set 124 -delete hostpci0`. Restore its exact saved net0 and on-boot setting. For example, use `pct set "$id" -net0 "$original_net0"` for a container. For VM 124, also restore its saved cloud-init IP configuration. Start the guest and verify original addresses, routes, and disks.
4. Restore the old guest tunnel routes and management DNS records through the previous deployed configuration. Confirm host and guest access from WARP. If reverting the overlay, release every VF attachment and remove the saved VF declaration through the node API first. Review a fresh plan against the restored configuration before resuming ordinary applies.
