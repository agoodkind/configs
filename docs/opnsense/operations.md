# OPNsense router operations

OPNsense runs as a BGP-only edge router, so the FRR routing daemon owns the kernel default route and the static WAN gateways do not. This page is the operating contract for the router: the steady state it holds, the foot-guns that break it, and how to recover. To stand up a replacement, treat the steady state below as the target and the foot-guns as the list to avoid.

## How the default route works

FRR installs and owns the kernel default route from BGP. MWAN peers announce a default route from a primary and a backup, and FRR selects the primary. No static WAN gateway is selected as the default, on purpose, because OPNsense would otherwise reinstall its own default and clobber the one BGP installed.

## Steady state

The router holds four pieces of configuration, and each exists to keep BGP's default route intact while the rest of the router works.

The NAT64 gateway, the translator that lets IPv6-only clients reach IPv4, is enabled but forced down. Forcing it down keeps it from ever winning default selection, while its static route still installs and the translator keeps working. That gateway carries one static route, which sends the NAT64 prefix to the translator.

Outbound NAT runs in manual mode with two rules, because OPNsense generates automatic NAT only when an interface has a configured gateway and none does here. One rule translates every internal network to the WAN address, and one translates the firewall's own outbound traffic. The internal-networks rule matches an alias that resolves to every internal interface, so a new VLAN added to that alias is covered without a rule change.

MWAN peers as two BGP neighbors per address family, a primary and a backup, and a route-map prefers the primary by local preference. FRR installs the winning default into the kernel, where it shows the flag that marks a zebra-installed route.

## Foot-guns

These are the ways an operator breaks the router, each with its cause and its fix.

Enabling any v4 or v6 gateway as a selectable default clobbers the BGP route. When a selectable default gateway exists, an OPNsense Apply reinstalls its own default route, and FRR's zebra does not see the delete, so its BGP default goes stale while the kernel holds whatever OPNsense reinstalled. Keep every gateway either deleted, disabled, or forced down, so no gateway is selectable as default and Apply leaves BGP's route alone. If you must enable a gateway briefly, restart FRR afterward to reinstall the BGP default.

Automatic outbound NAT also needs a gateway, which is why the manual rules exist. With no gateway on the WAN interface, OPNsense generates no automatic NAT for internal traffic, and the two manual rules cover that ground regardless of gateway state.

The NAT64 gateway must be forced down, never disabled. A disabled gateway is excluded from the route lookup, so its static route fails to install and NAT64 breaks; a forced-down gateway is still seen by the route lookup, so the route installs and translation keeps working, while it is skipped for default selection either way.

A GUI Apply behaves differently depending on gateway state. The default-route step runs only when a gateway is selectable, so it is safe as long as no gateway is selectable. The static-route rebuild always runs, but it only touches the specific networks its routes cover, not the default.

Two interface entries on the same untagged device silently drop one. OPNsense keys its interface map by device name, so the second entry overwrites the first, and the losing interface keeps its GUI config but binds no address to any kernel interface. Keep one interface per untagged device.

A firmware install needs a package upgrade before any package install. The install image ships a frozen package set while the mirror has moved on, so installing a package against the frozen set pulls a library built for a newer base and breaks tools such as `vtysh` with a version-mismatch error at startup. Run the package upgrade first, then install packages.

The Proxmox API refuses to set a VM's `args` field, which any virtio-serial VM needs. The check is hard-coded to allow only the bare root user, so no API token can set it. Create such a VM by hand as root over SSH, then import it into OpenTofu.

Hot-adding a NIC needs an interface reconfigure. Adding the NIC at the hypervisor makes the guest kernel see the new device, but the OPNsense interface config does not bind to it until you run an interface reconfigure on the guest for whichever interface should own the device.

## Recovery

Run these on the OPNsense router at `router.home.goodkind.io`.

When the BGP default route is wiped on both families, restart FRR and clear the stale kernel defaults so FRR reinstalls its own.

```bash
ssh agoodkind@router.home.goodkind.io 'sudo service frr stop'
ssh agoodkind@router.home.goodkind.io 'sudo route -n delete -inet default 2>/dev/null'
ssh agoodkind@router.home.goodkind.io 'sudo route -n delete -inet6 default 2>/dev/null'
ssh agoodkind@router.home.goodkind.io 'sudo service frr start'
sleep 5
ssh agoodkind@router.home.goodkind.io 'sudo netstat -rn | grep ^default'
```

The default entries should return with the zebra-installed flag.

When NAT64 stops working, confirm resolution and translation from a v6-capable LAN host, then check the NAT64 static route and the forced-down gateway on the router.

```bash
dig @3d06:bad:b01::64 +short AAAA ipv4.google.com
ping6 <synthesized address from above>
```

If DNS64 returns a synthesized address but the ping fails, the NAT64 static route is missing. Confirm the route exists on the router and that the NAT64 gateway is forced down rather than disabled.

When outbound NAT stops and LAN clients lose IPv4 while IPv6 still works, the manual NAT rules are most likely gone. Confirm them under Firewall, NAT, Outbound, and check the running rules on the router shell for a NAT rule on the WAN interface.

## Snapshots without saved RAM

Take every OPNsense snapshot without saved RAM, on production and testbed alike. A snapshot that saves RAM resumes on rollback with a stale wall clock, dead TCP sockets, and a stale resolver cache, which wastes hours chasing a failure that is really stale state. The web GUI defaults saved RAM on for a running VM, so do not take these snapshots from the GUI.

After a rollback, confirm the guest agent and serial console respond, SSH and the web UI answer, DNS resolves, and the default routes, firewall rules, and BGP state are sane before trusting the router again.
