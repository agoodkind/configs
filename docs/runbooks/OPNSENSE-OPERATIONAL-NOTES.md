# OPNsense Operational Notes

These are the non-obvious behaviors and configurations that matter when running
OPNsense as a BGP-only edge router. Static WAN gateways do not own the default
route. FRR owns the kernel default route.

If you stand up another OPNsense to replace this one, the steady-state config
section is the canonical end target. The operational rules section is the
list of foot-guns to avoid.

---

## Steady-state OPNsense config

Validated on prod (`router.home.goodkind.io`).

### Gateways (System / Gateways / Configuration)

| Gateway | Disabled | force_down | defaultgw | Notes |
|---|---|---|---|---|
| WAN_GW4 | (deleted) | n/a | n/a | Removed; BGP carries v4 default |
| WAN_GW6 | (deleted) | n/a | n/a | Removed; BGP carries v6 default |
| NAT64_GW | 0 | **1** | 0 | Enabled but force_down so it does NOT win default selection. Static route to `:6464::/96` still installs because the route lookup ignores `force_down`. Tayga keeps translating. |

### Static routes (System / Routes / Configuration)

| Network | Gateway | Why |
|---|---|---|
| `3d06:bad:b01:6464::/96` | NAT64_GW | NAT64 prefix delegated to Tayga for v6 to v4 translation |

Anything else here is cruft (e.g., a `:60` route pointing at a deleted
WAN_GW6 from mid-session recovery would log `gateway IP could not be found`
and silently skip install).

### Outbound NAT (Firewall / NAT / Outbound)

Mode: **Manual** (or Hybrid; either works as long as the manual rules below exist).

Auto-generated NAT does NOT run because the WAN interface no longer has
`<gateway>WAN_GW4</gateway>`. The `filter.inc` auto-NAT loop is gated on
`!empty($ifcfg['gateway'])`, so we must replace it with explicit manual rules.

| # | Interface | Source | Translation | Static port | Purpose |
|---|---|---|---|---|---|
| 1 | WAN | INTERNAL net (alias) | Interface address | NO | All LAN-side networks NAT to WAN address |
| 2 | WAN | THIS FIREWALL | Interface address | NO | OPNsense itself (loopback + interface IPs) NAT when egressing WAN |

INTERNAL net resolves to all v4 networks of every interface in the INTERNAL
group. When you add a new VLAN to INTERNAL, the rule auto-includes it.

THIS FIREWALL (`(self)` in pf) covers OPNsense-initiated outbound for
services that bind to loopback or any LAN-side address.

Optional rule for IPsec/IKE if used: same source as #1 but dst port 500 with
`Static port` checked (auto-NAT generates this; if you do IPsec, replicate it).

### Default routes (kernel, owned by FRR/zebra)

```
default            10.250.250.3       UG1          vtnet1   # BGP via VM 113 primary
default            3d06:bad:b01:fe::3 UG1          vtnet1   # BGP via VM 113 primary
```

`UG1` flag indicates installed by zebra (FRR). Status in `vtysh -c "show ip[v6] route 0.0.0.0/0"`
should be `Status: Installed, Selected` for the BGP entry.

### FRR / Quagga

- Plugin: `os-frr` installed
- BGP mode enabled
- Two neighbors per family:
  - `10.250.250.3` and `3d06:bad:b01:fe::3` (VM 113, primary, route-map sets local-pref 200)
  - `10.250.250.4` and `3d06:bad:b01:fe::4` (LXC 116, backup, route-map sets local-pref 100)

### VRRP

No keepalived or VRRP is in the steady-state design. BGP carries the default
route directly to VM 113's real address `.3` / `:fe::3`.

---

## Operational rules (foot-guns)

### Rule 1: Never enable a v4 or v6 gateway entity at top priority

Source evidence: `src/etc/inc/system.inc` lines 703-723 + 651-654.

```php
$gateway = $gateways->getDefaultGW($down_gateways, $ipproto);
if (empty($gateway['gateway'])) {
    continue;       // SKIP because no default gateway candidate
}
...
system_default_route($gateway, $routes);  // route delete + add
```

`system_routing_configure` runs `system_default_route` for each address family
**only if** `getDefaultGW` returns a non-empty gateway. `system_default_route`
unconditionally calls:

```
route delete -inet6 default
route add -inet6 default <new_gateway>
```

zebra on FreeBSD does not see `RTM_DELROUTE`, so its BGP-installed default
stays `Status: None` while the kernel sits with whatever OPNsense reinstalled
(or nothing). Recovery requires `service frr stop && route delete + route start`.

| Gateway state for family | system_default_route called? | BGP route survives Apply? |
|---|---|---|
| Any enabled non-down gateway exists | YES | NO (kernel default replaced) |
| All gateways for family are deleted/disabled/force_down | NO (early `continue`) | YES |

So: with WAN_GW4/GW6 deleted and NAT64_GW force_down, no v4 or v6 gateway is
selectable as default. Future Apply events skip `system_default_route` and
leave BGP's kernel default untouched.

### Rule 2: Auto outbound NAT also requires a gateway

Source: `src/etc/inc/filter.inc` lines 220-238.

```php
foreach ($fw->getInterfaceMapping() as $intf => $ifcfg) {
    if (substr($ifcfg['if'], 0, 4) != 'ovpn' && !empty($ifcfg['gateway'])) {
        // generate auto outbound NAT rule
    }
}
```

When WAN_GW4 was deleted, the `<wan><gateway>` field cleared, so this
condition went false. Auto-NAT for v4 LAN to WAN stopped generating. v4 LAN
clients lost Internet because their packets were forwarded but not source-NAT'd.

Replacement: the two manual rules in the steady-state section. They cover
the same ground regardless of gateway state.

### Rule 3: NAT64_GW disabled vs force_down

| Setting | Effect on `getDefaultGW` | Effect on static route install |
|---|---|---|
| `disabled=1` | Skipped (good, doesn't grab default) | **Skipped**. `gatewaysIndexedByName(false, ...)` excludes disabled gateways. The `:6464::/96` static route logs `gateway IP could not be found` and is NOT installed. NAT64 breaks. |
| `disabled=0` + `force_down=1` | Skipped (good) | **Installed**. `gatewaysIndexedByName` ignores `force_down`. NAT64 keeps working. |

Use `force_down`, not `disabled`, when you want a gateway to exist for
static-route reference but never be selected as default.

### Rule 4: If you must enable a gateway temporarily

Restart FRR after to recover BGP defaults:

```
ssh agoodkind@3d06:bad:b01::1 "sudo service frr stop && sudo route -n delete -inet default 2>/dev/null; sudo route -n delete -inet6 default 2>/dev/null; sudo service frr start"
```

(Adjust which family to delete based on which gateway you enabled.)

### Rule 5: GUI Apply consequences are conditional

For most gateway/route/interface changes, the Apply triggers
`system_routing_configure`. The defaults steps run per the rules above.
The static-route rebuild always runs (delete + add for every
`<staticroutes><route>` entry) but only affects whatever specific networks
those routes cover, not the default. So it's safe assuming Rule 1 holds.

---

## Recovery snippets

### BGP default got wiped on v4 + v6

```
ssh agoodkind@3d06:bad:b01::1 'sudo service frr stop'
ssh agoodkind@3d06:bad:b01::1 'sudo route -n delete -inet default 2>/dev/null'
ssh agoodkind@3d06:bad:b01::1 'sudo route -n delete -inet6 default 2>/dev/null'
ssh agoodkind@3d06:bad:b01::1 'sudo service frr start'
sleep 5
ssh agoodkind@3d06:bad:b01::1 'sudo netstat -rn | grep ^default'
```

Should show BGP defaults restored (`UG1` flag).

### NAT64 stops working

Check (from any v6-capable host on LAN):

```
dig @3d06:bad:b01::64 +short AAAA ipv4.google.com
ping6 <synthesized addr from above>
```

If DNS64 returns synth but ping fails, check the `:6464::/96` static route
on OPNsense (`netstat -rn -f inet6 | grep 6464`). If missing, check that
NAT64_GW is `disabled=0` AND the static route is `disabled=0`.

### Outbound NAT stops working (LAN clients lose v4 Internet but v6 works)

Most likely cause: someone enabled then disabled WAN_GW4, or the manual
NAT rules got deleted. Check Firewall / NAT / Outbound for the two
manual rules. Verify with `pfctl -sn | grep ^nat` from OPNsense shell;
should see at least one `nat on vtnet1 inet from <internal_net> to any -> (vtnet1:0)` rule.
