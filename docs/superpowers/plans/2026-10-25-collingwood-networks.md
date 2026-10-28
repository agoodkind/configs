# Collingwood network migration implementation plan

> Use `executing-plans` to implement each task and track its steps with checkboxes.

**Goal:** Separate household networks while preserving remote administration and existing DNS names.

**Architecture:** PowerEdge provides routing, Kea DHCP, BIND DNS, and IPv6 advertisements. UniFi switches tagged household networks, and dedicated VF interfaces provide VMnet and temporary DAC connections.

**Tech Stack:** OpenTofu, pveguest, systemd-networkd, FRR, Kea, BIND, nftables, and UniFi Network.

**Spec:** [Approved network layout](#approved-network-layout).

## Approved network layout

Each IPv4 subnet uses `/24`, and each IPv6 prefix uses `/64`. Every new household gateway uses `.1` and `::1`. DNS suffixes end in `collingwood.goodkind.io`.

| Network | Connection | IPv4 | IPv6 | Target DNS suffix |
|---|---|---|---|---|
| Personal management | Existing untagged LAN; reserve VLAN 1001 for a later cutover | 10.230.0.0 | 3d06:bad:b01:300:: | management |
| Privileged | VLAN 101 | 10.230.1.0 | 3d06:bad:b01:301:: | privileged |
| General | VLAN 102 | 10.230.2.0 | 3d06:bad:b01:302:: | general |
| Guest | VLAN 103 | 10.230.3.0 | 3d06:bad:b01:303:: | guest |
| VMnet | Dedicated internal VF interfaces | 10.230.4.0 | 3d06:bad:b01:304:: | vmnet |
| Temporary DAC | Dedicated nic3 VF | 10.230.5.0 | 3d06:bad:b01:305:: | dac |

## Global constraints

- Preserve management and DAC addresses, the router's link-local identity, WAN selection, and MWAN transit.
- Preserve directly assigned VFs and use proven NIC hardware switching and supported offloads. Do not introduce Linux bridges in forwarding.
- Grant broad access to every device on VLAN 101. Select VLAN membership individually.
- Assign ordinary client access ports to General VLAN 102 after controlled tests. Keep management equipment and the PowerEdge uplink on their reviewed native network.
- Keep the current management network untagged until a separate reviewed cutover. Preserve PowerEdge's management VF and avoid duplicate management subnets on isolated interfaces.
- Permit General and Guest Internet access and required router services. Block their new connections to other local networks unless an explicit rule permits them.
- Keep BIND local and Cloudflare responsible for selected public services. Preserve Kea leases, dynamic records, and old names during migration.
- Keep the existing LAN's DHCP, DNS, and advertisements operational until its household clients migrate. Management retains its IPv6 prefix.
- Coordinate live changes with the state-migration task and use the authoritative backend it verifies. Do not apply while its deployment hold remains active.
- Keep secret values out of source, plans, logs, and chat. Provider development remains separate in LAB-142.

## Review focus

- A network reload must preserve existing management and DAC addresses and the router's IPv6 link-local identity.
- New VLAN links must not accept clients before the new firewall rules apply.
- Kea must retain the existing subnet identifiers, lease timings, and names for current clients.
- BIND must load fresh zone files before accepting new DHCP updates and must preserve its existing dynamic journals.
- Reverting a client assignment must restore the original address configuration and access without requiring physical assistance.

## Task 1: Prepare additive router configuration, LAB-144

- [x] Define the three household VLANs in [collingwood.tf](../../../opentofu/modules/poweredge/lan/collingwood.tf). Derive network files, DHCP scopes, DNS zones, and route advertisements from that definition.
- [x] Add systemd-networkd VLAN devices and a drop-in for the existing LAN VF. Prepare native reload and reconfigure commands without resetting VFs or restarting the network daemon.
- [x] Preserve existing DHCP configuration and append new subnet identifiers 101, 102, and 103. Use the existing lifetime settings for both address families.
- [x] Add the new BIND listeners, forward and reverse zones, Kea update domains, and FRR advertisements. Seed new dynamic zone files once and preserve subsequent Kea updates.
- [x] Add interface rules for Privileged, General, and Guest while preserving the existing LAN and DAC rules during migration. Apply the validated firewall before activating new VLAN links.
- [x] Separate firewall activation from VLAN activation with explicit service dependencies. Derive firewall interface groups from each network's access policy.
- [x] Run the native Kea, BIND, FRR, nftables, and systemd validators against rendered files. Verify authenticated DNS updates and allowed and denied packets in isolated Linux namespaces.
- [x] Run `./configsctl lint` and canonical OpenTofu format and validation checks.
- [x] Commit the coherent source change.

## Task 2: Prepare and deploy through the released backend

- [x] Obtain the state-migration completion notice and verify the supported workspace selector.
- [x] Verify independent DAC administration and preserve protected backups of live network configuration, leases, DNS data, and UniFi profiles.
- [x] Review a fresh canonical plan. Reject incidental VF resets, container replacement, management address changes, WAN changes, and service changes outside the migration.
- [x] Verify that replacing the old firewall tracking resource does not stop the active service. The pinned provider's delete operation forgets state, and the new declaration adopts the active service without restart triggers. Keep rule updates atomic through the activation unit.
- [x] Apply only the reviewed configuration. Confirm the existing administration session, an independent session, DHCP, DNS, Internet access, BGP, and Cloudflare health.
- [x] Record the applied revision and current verification. Complete LAB-144 after deployment and remote access checks pass.

## Task 3: Configure UniFi, LAB-146

- [x] Inspect actual port and Wi-Fi assignments before changing them.
- [x] Configure third-party-gateway networks 101, 102, 103, and 1001. Preserve the current untagged network until the reviewed management cutover while permitting the new VLAN tags.
- [x] Test the three household VLAN tags through temporary VLAN interfaces on a wired client while preserving its untagged connection. Remove those temporary configurations after verification.
- [ ] Migrate selected client ports or Wi-Fi networks only after their controlled tests pass. Keep current management untagged.

## Renumber the prepared household VLANs

Linux cannot change an existing VLAN interface's ID. A successful network reload does not prove the requested IDs took effect. The [networkctl reference](https://www.freedesktop.org/software/systemd/man/257/networkctl.html) requires deleting the old interface before reloading its changed definition.

- [x] Verify the dedicated DAC control connection and save the current router and UniFi configuration in protected backups.
- [x] Inspect active leases and switch assignments before replacing the household VLAN interfaces. Preserve the existing untagged management network and DAC connection.
- [x] Review a canonical `poweredge/routing` plan containing only the three `.netdev` changes and their network activation trigger. Exclude the unrelated MWAN agent restart.
- [x] Stage all three `.netdev` files through the canonical provider before activation. Keep the saved-plan source unchanged during each apply.
- [x] Verify that `privileged`, `general`, and `guest` are VLAN children of `lan-out`. Delete only those three interfaces, then activate the reviewed network unit. Preserve the parent VF, management addresses, and DAC VF.
- [x] Reopen Kea's DHCPv4 and DHCPv6 sockets after the interfaces restart. Check actual DNS responses before restarting BIND for stale interface bindings.
- [x] Verify runtime VLAN IDs 101, 102, and 103, unique connected subnets, BGP return routes, both Internet families, local DNS, and both Cloudflare connectors.
- [x] Record the activation trigger through the canonical provider and require a clean router-module plan.
- [ ] Test native VLAN 1001 and the tagged networks on a controlled port before changing existing native port assignments. Router and switch updates do not share an atomic transaction.

If verification fails, restore the saved `.netdev` files, replace only the three household VLAN interfaces, activate the network unit, and reopen the DHCP sockets. Restore the reviewed UniFi profiles if the switch phase changed them. Test the DAC connection and original management service before resuming.

## Task 4: Verify hardware and isolation, LAB-147

- [ ] Test both address families, router advertisements, DHCP renewal, local DNS, Internet access, and return routes from controlled clients.
- [ ] Exercise permitted connections, prohibited connections, and replies after hardware rules exist. Verify that the NIC cannot bypass the router's access rules.
- [ ] Compare throughput, device counters, and CPU usage against the current path. Report 100% CPU as one core and distinguish hardware switching from software routing.

## Task 5: Migrate clients, LXCs, and DNS

- [ ] Activate the management and DAC DNS suffixes only after testing client-supplied fully qualified names and preserving existing dynamic records. The target suffixes in the layout do not establish live DHCP advertisement.
- [ ] Complete LAB-145 with tested old-name compatibility before changing existing client names. Include clients that submit full old names during DHCP renewal.
- [ ] Complete LAB-148 one port or Wi-Fi group at a time. Confirm new addresses and active routes before migrating another group.
- [ ] Complete LAB-149 one LXC at a time after verifying a separate internal VF network and its isolation. Do not assume that adding an interface isolates it.
- [ ] Complete LAB-150 after checking lease expiration, static addresses, and observed dependencies. Retain management addresses and the `:300` prefix.

## Recover a failed migration group

Restore the group's previous UniFi assignment and renew its network settings. Preserve the old router and DNS configuration during the transition. Use the independent DAC connection to restore the validated configuration if router activation fails. Verify both address families and management before resuming migration.
