# Test the tertiary WAN on suburban

The failover guest restarts during this procedure. Its BGP session drops until
the guest starts again. Use the suburban testbed for these checks.

Pin a release containing [MWAN PR 264](https://github.com/agoodkind/mwan/pull/264)
in the [testbed release configuration](../../../ansible/inventory/group_vars/mwan_testbed_all.yml)
before deploying either speaker.

## Test the primary speaker

1. Run `./configsctl deploy deploy-mwan --limit mwan_suburban_servers`.
2. Confirm that WANCONFIG reports `tertiary` at tier 1 with IPv4 NAPT44,
   IPv6 masquerade, and an address-only DHCPv6 request.
3. Make the tier 0 simulator uplinks unhealthy. Confirm that new client
   connections use `tertiary` for both address families. Capture packets on
   the tertiary ISP simulator and confirm that each source address matches
   the primary speaker's acquired address for that family.
4. On the tertiary ISP simulator, run `systemctl stop kea-dhcp6-server` and
   keep `radvd` running. Restart the primary speaker and repeat the client
   traffic and packet capture checks from step 3. Confirm that DHCPv6 is
   waiting for a lease and that IPv6 connections use the acquired SLAAC address.
5. On the simulator, run `systemctl start kea-dhcp6-server` and restore the
   tier 0 uplinks. Confirm that new
   connections use the preferred healthy providers again.

## Test the failover speaker

1. Run `./configsctl tofu suburban/mwan plan` and review the guest 216 update.
   Confirm that the plan removes DHCP from the first interface and preserves
   the transit and management interfaces.
2. Apply the reviewed OpenTofu change with `./configsctl tofu suburban/mwan apply`.
3. Run `./configsctl deploy deploy-mwan-failover --limit mwan_failover_suburban_servers`.
4. Restart guest 216 on suburban with `ssh suburban 'pct reboot 216'`.
5. Check `networkctl status eth0` inside the guest. Confirm that
   systemd-networkd manages the interface and installs the IPv4 address,
   IPv6 address, and default routes supplied by the simulated ISP.
6. Confirm that the OPNsense BGP session recovers and that IPv4 and IPv6
   traffic from a testbed client passes through the failover guest. Capture
   packets on the simulated ISP and check that their source addresses match
   the guest's uplink addresses.
7. On the tertiary ISP simulator, run `systemctl stop kea-dhcp6-server` and
   keep `radvd` running. Repeat steps 4 through 6. Confirm that the guest
   does not retain a DHCPv6 address and forwards IPv6 using its SLAAC address.
8. On the simulator, run `systemctl start kea-dhcp6-server` and verify that the failover guest
   acquires an IPv6 address. Exercise the SLAAC health recovery toggle and
   confirm that IPv6 addresses, the default route, and forwarding recover.
