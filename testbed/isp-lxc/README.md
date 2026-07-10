# isp-lxc templates

Jinja2 templates rendered onto ISP simulator LXCs 200 (Webpass), 201 (AT&T),
and 202 (Monkeybrains / mbrains) by the testbed deploy playbook
(`deploy-testbed.yml` via `tasks/deploy-testbed-isp-lxc.yml`). Each LXC's
capabilities are driven by its entry in `testbed_isp_lxcs`
(`ansible/inventory/group_vars/suburban_servers.yml`): `pd_len`, `dynamic_v4`,
`ia_na`, `slaac_prefix`, `v4_reservations`.

## Files

- `radvd.conf.j2`: radvd configuration. Always advertises the router (default
  route) with `AdvManagedFlag on` + `AdvOtherConfigFlag on` so the WAN VM runs
  DHCPv6 for IA_NA and PD. When `slaac_prefix` is set, it also advertises that
  /64 with `AdvAutonomous on` so the VM autoconfigures a SLAAC global address,
  mirroring prod where the ISP RA yields a SLAAC `mngtmpaddr`.
- `kea-dhcp6.conf.j2`: DHCPv6. Always delegates a prefix (`pd_len` long). When
  `ia_na` is set, it also hands out IA_NA addresses from a pool on the
  `slaac_prefix` /64 (full prod-parity dynamic stack).
- `kea-dhcp4.conf.j2`: DHCPv4. Rendered and enabled only when `dynamic_v4` is
  set (the `kea-dhcp4-server` unit is pre-installed on the LXCs). Serves the
  `v4_subnet` pool with optional `v4_reservations` host reservations.
- `nftables.conf.j2`, `nftables.conf.tmpl`: firewall and NAT rules for ISP LXCs.
- `pd-route.service.j2`, `pd-route.service.tmpl`: systemd unit that installs the
  delegated-prefix return route after the DHCP-PD exchange completes.
- `sysctl-isp.conf`: kernel tuning applied to ISP LXCs.
- `200/`, `201/`, `202/`: rendered per-LXC reference snapshots.

## Per-ISP IPv6/IPv4 behavior

Capabilities mirror how prod addresses each WAN:

- Monkeybrains (202): full dynamic stack. DHCPv4 + DHCPv6 IA_NA + DHCPv6-PD
  (`/56`) + SLAAC (on `slaac_prefix`). v4 egress is masquerade.
- AT&T (201) and Webpass (200): DHCPv6-PD + RA only (no `slaac_prefix`, no
  `dynamic_v4`); the WAN VM holds a static v4. These reach full prod parity in
  later phases (att dynamic link plus routed static /29, webpass static /29).
