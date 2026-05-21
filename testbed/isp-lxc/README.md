# isp-lxc templates

Jinja2 templates rendered onto ISP simulator LXCs 200 (Webpass), 201 (AT&T),
and 202 (Monkeybrains / mbrains) by the testbed deploy playbook.

## Files

- `radvd.conf.j2`: radvd configuration. Advertises the delegated prefix with
  `AdvAutonomous on` so SLAAC clients auto-configure a global IPv6 address.
- `kea-dhcp6.conf.j2`: DHCPv6 PD-only. No IA_NA pool; clients rely on SLAAC
  for global unicast addressing.
- `nftables.conf.j2`, `nftables.conf.tmpl`: firewall and NAT rules for ISP LXCs.
- `pd-route.service.j2`, `pd-route.service.tmpl`: systemd unit that installs the
  delegated-prefix return route after the DHCP-PD exchange completes.
- `sysctl-isp.conf`: kernel tuning applied to ISP LXCs.
- `200/`, `201/`, `202/`: per-LXC variable overrides consumed by the templates.

## IPv6 Behavior

ISP simulator LXCs advertise delegated prefixes with SLAAC enabled. Failover
clients receive their global IPv6 source address from RA, and DHCPv6-PD handles
prefix delegation only.
