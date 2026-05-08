# isp-lxc templates

Jinja2 templates rendered onto ISP simulator LXCs 200 (Webpass), 201 (AT&T),
and 202 (Monkeybrains / mbrains) by the testbed deploy playbook.

## Files

- `radvd.conf.j2`: radvd configuration. Advertises the delegated prefix with
  `AdvAutonomous on` so SLAAC clients (e.g. LXC 100 eth0) auto-configure a
  global IPv6 address.
- `kea-dhcp6.conf.j2`: DHCPv6 PD-only. No IA_NA pool; clients rely on SLAAC
  for global unicast addressing.
- `nftables.conf.j2`, `nftables.conf.tmpl`: firewall and NAT rules for ISP LXCs.
- `pd-route.service.j2`, `pd-route.service.tmpl`: systemd unit that installs the
  delegated-prefix return route after the DHCP-PD exchange completes.
- `sysctl-isp.conf`: kernel tuning applied to ISP LXCs (forwarding, RA settings).
- `200/`, `201/`, `202/`: per-LXC variable overrides consumed by the templates.

## MWAN-65: AdvAutonomous fix (2026-05-08)

Root cause: `radvd.conf.j2` had `AdvAutonomous off`, which prevented SLAAC
clients from auto-configuring a global IPv6 address from the advertised prefix.
`kea-dhcp6.conf.j2` is PD-only with no IA_NA pool, so there was no DHCPv6
fallback either. The net effect was that LXC 100's eth0 carried only a
link-local IPv6 address on the ISP-side bridge. The nftables `masquerade` rule
on eth0 could not pick a valid global source, so v6 traffic forwarded through
LXC 100 during failover exited with the internal `3d06:bad:b01:201::4` source
and was black-holed by the ISP simulator. This produced 70.7% v6 packet loss
versus 15.6% v4 loss during the 2026-04-27 testbed drill.

Fix: set `AdvAutonomous on` in `radvd.conf.j2`. After re-rendering and applying
on LXC 200/201/202, LXC 100's eth0 will SLAAC a global address from the
delegated prefix and masquerade will work correctly. This matches production
Monkeybrains behavior, where the real upstream also advertises `AdvAutonomous on`.

This is Option B from the investigation report at
`mwan/docs/MWAN-65-v6-loss-asymmetry-investigation.md`, section 5.
