# MWAN-72: when does `system_routing_configure` wipe BGP defaults?

Status: read-only investigation, May 7 2026.
Prod version: OPNsense 25.7.11_9 (`amd64`).
Vendor master ABI: 26.1 (`opnsense/core` `Mk/version.mk` declares `CORE_ABIS?= 26.1`).

The testbed VM 101 was unreachable from the workstation during this read window
(`ssh: connect to host 3d06:bad:b01:200::11 port 22: Network is unreachable`),
and reaching it via suburban requires an interactive credential the read-only
session does not have. The 25.7 source on prod and the 26.1 source on
`opnsense/core@master` were both readable, and that is what this report relies
on. Section 6 calls out the gap that requires a live testbed run.

The earlier scratch notes in `mwan/OPNSENSE-OPERATIONAL-NOTES.md` were used as
a starting point and verified line-for-line against `system.inc` and
`Gateways.php` on prod. Where this report quotes line numbers, they refer to
`/usr/local/etc/inc/system.inc` and
`/usr/local/opnsense/mvc/app/models/OPNsense/Routing/Gateways.php` on prod
(25.7.11_9), which match `opnsense/core@master` in every load-bearing line.

---

## 1. Trigger conditions

`system_routing_configure(...)` is the function that decides whether to call
`system_default_route(...)`. The entrypoints found on prod:

| Caller | Line / location | Context |
|---|---|---|
| `/usr/local/etc/rc.bootup` | line 94 | Boot-time, with `monitor='ignore'` so down-gateway detection is bypassed. |
| `/usr/local/etc/rc.reload_all` | line 54 | Full reconfigure (e.g. firmware upgrade finalize, "Reload all" path). |
| `/usr/local/etc/rc.routing_configure` | line 51 | The shim invoked by `configd` after a Gateways/Routes/Apply event. Also invoked when `dpinger` reports a gateway state change with `gw_switch_default` enabled. |
| `/usr/local/etc/rc.newwanip` | lines 101, 105, 123 | DHCPv4/v6 lease change, PPP up, interface re-attach. Calls per-family. |
| `/usr/local/etc/rc.newwanipv6` | line 120 | IPv6 lease/prefix change. |
| `/usr/local/opnsense/scripts/shell/setports.php` | line 44 | After a console interface re-assignment. |
| `/usr/local/opnsense/scripts/interfaces/ppp-ipv6.php` | line 61 | PPP IPv6 up event. |
| `/usr/local/etc/inc/interfaces.inc` | line 2531 | Inside per-interface configure path (`interface_configure(...)` or similar wrappers). |
| `/usr/local/etc/inc/interfaces.inc` | line 3730 | Inside the bulk reload path that walks `restartifs`. |

The Quagga/FRR plugin is not in this list. `frr.inc` only registers a
`quagga restart` service hook and does not call `system_routing_configure`.
That detail matters for MWAN-13: an FRR config push by itself does not retrigger
the routing apply pipeline. The triggers above are firewall events, gateway
events, and link/lease events.

The "Save" button under System / Gateways / Configuration writes
`<gateway_item>` to `/conf/config.xml` and runs `interface gateways
configure`, which boils down to `rc.routing_configure`. Same thing for System /
Routes / Configuration "Apply" and any GUI path that re-runs the gateways
backend.

The tag `monitor='ignore'` (used by `rc.bootup`) skips the
`return_gateways_status()` walk that builds the `$down_gateways` set, so it
also skips the `force_down` filter; in that branch the only reason a gateway is
excluded is the `getDefaultGW()` skip-list (next section).

---

## 2. What gets wiped

The wipe is concentrated in `system_default_route($gw, $routes)`,
`system.inc:610-657`. The relevant block is short:

```php
mwexecfm('/sbin/route delete -%s default', [$family]);
mwexecf('/sbin/route add -%s default %s', [$family, $gateway]);
```

That is an unconditional `route delete` followed by a `route add`. The earlier
"keeping {$family} default route to {$gateway}" branch only short-circuits when
the existing default route in the kernel already points at the gateway the
function would install. zebra's BGP-installed default has a *different*
gateway (the BGP next-hop, e.g. `10.250.250.3`), so that loop never matches and
the delete-then-add path runs every time.

Concretely, what disappears at the moment `system_default_route` runs:

- The kernel default route for the matching family.
- The BGP-derived default route in zebra's view of the kernel state, because
  zebra on FreeBSD does not act on the `RTM_DELROUTE` it would need to notice
  the kernel-side delete. After the OPNsense reinstall, `vtysh -c "show ip[v6]
  route 0.0.0.0/0"` reports the BGP entry as `Status: None` even though the BGP
  session is up. Recovery is `service frr stop && route delete && service frr
  start` (or, equivalently, `service frr restart` if no leftover stale kernel
  default exists).
- Anything else BGP advertises is unaffected, since `system_default_route`
  literally only touches `default`/`::default`.

The static-route rebuild later in `system_routing_configure` (lines 730-790)
also runs every invocation. For each `<staticroutes><route>` entry it does:

```php
mwexecfm('/sbin/route delete ' . implode(' ', $cmd));
...
mwexecfm('/sbin/route add ' . implode(' ', $cmd));
```

That delete-then-add affects only the prefix in `<network>`, so it does not
touch the BGP default. It does interact with NAT64: the
`3d06:bad:b01:6464::/96` route to `NAT64_GW` is reinstalled by this loop, and
the `gatewaysIndexedByName(false, true)` call upstream determines whether
`NAT64_GW` is included.

What does *not* get wiped by this function: BGP neighbor sessions, FRR config
files under `/usr/local/etc/frr/`, FRR's RIB or AS path tables, BFD state, and
any non-default static route that does not appear in the static-routes loop.

---

## 3. Conditional logic

The "selective on getDefaultGW" phrase in the ticket title points at this
block (`system.inc:696-720`):

```php
foreach (['inet' => 'ipv4', 'inet6' => 'ipv6'] as $ipproto => $type) {
    if ($family !== null && $family !== $ipproto) {
        continue;
    }

    $gateway = $gateways->getDefaultGW($down_gateways, $ipproto);
    if (empty($gateway['gateway'])) {
        continue;
    }

    if (isset($config['system']['gw_switch_default']) || empty($interface_map) || in_array($gateway['interface'], $interface_map)) {
        if (empty($ifdetails[$gateway['if']][$type][0])) {
            log_msg("ROUTING: refusing to set {$ipproto} gateway on addressless {$gateway['interface']}({$gateway['if']})", LOG_ERR);
            continue;
        }

        log_msg("ROUTING: configuring {$ipproto} default gateway on {$gateway['interface']}", LOG_INFO);

        system_default_route($gateway, $routes);
    }
}
```

`getDefaultGW` is at `Gateways.php:490-510` and skips a gateway when any of
these is true: it is in the caller-provided skip list (the `$down_gateways`
array, populated from `return_gateways_status()`), or `disabled`, or `defunct`,
or `is_loopback`, or `force_down`. It returns `null` when no gateway survives
the filter for the given protocol family.

The skip-when-no-default-gw guard is the line `if (empty($gateway['gateway']))
{ continue; }`. That is the load-bearing conditional. With WAN_GW4 and WAN_GW6
deleted from `/conf/config.xml` and NAT64_GW marked `force_down`, this
function returns `null` for both `inet` and `inet6`, the `continue` fires, and
`system_default_route` is never invoked. The kernel default that zebra
installed for BGP is left in place.

A second-tier filter is the `isset($config['system']['gw_switch_default'])`
clause. When `gw_switch_default` is unset (the prod case as of May 7 2026,
verified via `xmllint --xpath "//system/gw_switch_default" /conf/config.xml`
returning `XPath set is empty`) and `$interface_map` is empty, the path still
calls `system_default_route` whenever `getDefaultGW` returned a non-null
gateway. So `gw_switch_default` is not a safety net for our use case; it
controls whether dpinger-driven gateway switches participate, not whether the
default-route reinstall happens at all.

A truth table for the prod config:

| Gateway state for family | `getDefaultGW` returns | `system_default_route` called? | BGP kernel default survives Apply? |
|---|---|---|---|
| Any active, non-down, non-`force_down`, non-disabled gateway | gateway record | yes | no, kernel default is delete-then-added |
| Every gateway for the family is deleted, disabled, or `force_down` | `null` | no, `continue` fires | yes |

There is one subtlety worth recording. `gatewaysIndexedByName(false, true,
false)` in the static-route rebuild treats `force_down` as a non-filter (it
filters on `disabled`, `is_loopback`, `if`-empty, but not on `force_down`).
That is why a `force_down` NAT64_GW still installs the `:6464::/96` static
route while never being eligible to win the default selection.

---

## 4. Version diff (25.7 vs 26.1 master)

The two files we care about are:

- `src/etc/inc/system.inc`
- `src/opnsense/mvc/app/models/OPNsense/Routing/Gateways.php`

For `system.inc`, the `system_routing_configure` function on `master` and on
prod 25.7 are byte-for-byte identical in every load-bearing line. The only
change inside the function since 25.7 went stable is a comment-only edit at
line 761 (commit `3acfb5f2`, April 13 2026): a `/* XXX: get_staticroutes logic
works if field is named enabled or disabled. ... */` annotation above the
static-route disabled check. No behavior change.

For `Gateways.php`, the latest commit touching the file (`dc357ece`, May 5
2026) refactors `getRealInterface` from a private instance method to a static
utility on the `Util` class. The change is internal: it does not alter
`getDefaultGW`, `gatewaysIndexedByName`, or any of the conditional logic that
controls whether `system_default_route` runs. Both functions on `master` are
identical to the prod 25.7 copy.

That means the wipe behavior in section 2 and the conditional logic in section
3 carry forward to 26.1 unchanged. There is no hidden 26.x guard that protects
BGP-installed defaults, and there is no hidden 26.x change that makes the
behavior worse.

`opnsense/core` does not have a `stable/25.7` branch on github (the listing
returned `stable/15.7` only, since the OPNsense versioning scheme drops the
"20" prefix; `stable/15.7` is the production 25.7 branch). Pulling the
version file from that branch returned 404 in this environment (the file
references `%%CORE_ABI%%` placeholder substitution that only resolves at build
time), but the source files on `stable/15.7` track 25.7 releases.

A focused git-log of `system.inc` since July 2025 shows no commit that adds or
modifies a default-route guard, and the `Gateways.php` log shows no change to
the `getDefaultGW` filter set.

---

## 5. Implications for MWAN-13 (26.x upgrade)

The behavior carries over unchanged, so the steady-state posture documented in
`mwan/OPNSENSE-OPERATIONAL-NOTES.md` is the right starting point for the 26.1
cutover. The runbook does need to handle the upgrade-time triggers explicitly,
because a firmware upgrade finalize calls `rc.reload_all`, which calls
`system_routing_configure(true)` with `$interface_map=null` and `$monitor=true`
(line 54 of `rc.reload_all`).

State to capture before the upgrade window (read-only, can be done from the
operator workstation against prod):

- `sudo netstat -rnf inet | head` and `sudo netstat -rnf inet6 | head`,
  recording the `UG1` flag and the BGP next-hop on each default.
- `sudo vtysh -c "show ip route 0.0.0.0/0"` and the `ipv6` equivalent, noting
  `Status: Installed, Selected` on the BGP entry.
- `sudo /usr/local/sbin/configctl interface gateways list` (this returns the
  active gateway set, not just `<gateway_item>` entries; on prod May 7 2026
  this returns `{"NAT64_GW": "...", "Null6":"...", "Null4":"..."}`).
- `xmllint --xpath "//gateways" /conf/config.xml` to confirm the
  `<gateway_item>` set is still empty, and `xmllint --xpath
  "//system/gw_switch_default" /conf/config.xml` to confirm it is unset.
- `vtysh -c "show running-config"` snapshot for FRR.
- The current `/conf/config.xml` (full backup before any upgrade).

What to verify after the upgrade:

- `netstat -rnf inet[6]` shows the BGP defaults restored with `UG1`. If the
  default is gone or the gateway pointed at something other than the BGP
  next-hop, the wipe-and-recover path of section 2 fired during finalize.
- `vtysh -c "show ip route 0.0.0.0/0"` reports `Status: Installed, Selected`.
  If it reports `Status: None` while the BGP session is up, the recovery
  snippet in `OPNSENSE-OPERATIONAL-NOTES.md` is the right tool.
- `<gateway_item>` set is still empty in `/conf/config.xml`, no upgrade-time
  schema migration silently re-introduced a WAN_GW4/GW6 entry.
- NAT64 still works (`netstat -rnf inet6 | grep 6464` shows the `/96` route to
  the NAT64_GW link-local).

Guardrails to add to the 26.1 runbook:

- Pre-flight: assert `<gateway_item>` is empty (or all entries have
  `<force_down>1</force_down>` or `<disabled>1</disabled>` for both families).
  If the assertion fails, abort and fix gateway config first, since an active
  WAN gateway will cause `system_routing_configure` during finalize to install
  a kernel default pointing at the static gateway and zebra will not notice.
- Pre-flight: assert `gw_switch_default` is unset. With it set, dpinger-driven
  gateway switches during the finalize window can also retrigger the default
  reinstall.
- During finalize: be ready to run the recovery snippet from
  `OPNSENSE-OPERATIONAL-NOTES.md` (`service frr stop`, `route delete`,
  `service frr start`) if BGP defaults are missing post-finalize. This is the
  same recovery the cutover runbooks already ship.
- Out-of-band path: keep the OOB tunnel on `3d06:bad:b01:ff::1` reachable
  during the window so the operator can run the recovery without depending on
  in-band v4 default-route reachability.

---

## 6. Open questions

The static read of source files cannot answer these by itself, so they are
candidates for a live testbed run before the production upgrade window:

- The 26.1 finalize/upgrade path may invoke `system_routing_configure` more
  than once (`rc.reload_all` plus per-interface configures during the
  pkg-upgrade dance). The exact number of invocations and the resulting
  number of BGP wipe-and-recover cycles is not knowable from source alone.
  The testbed VM 101 should be brought up on 26.1 with the same
  `<gateway_item>` posture as prod and observed across an upgrade-style apply.
- The interaction with `gw_switch_default` is documented in code but not
  exercised on prod (where it is unset). MWAN-13 should decide whether the
  upgrade window should also leave it unset, or whether enabling it would
  give the FRR-driven setup any benefit. The current evidence says no, but a
  testbed run with it enabled and dpinger flap would confirm.
- The `force_down` semantics are confirmed for the default-selection path.
  They are not exhaustively tested for the static-route loop on 26.1; the
  vendor change at `gatewaysIndexedByName` is unchanged but the
  `getRealInterface` refactor in `dc357ece` could in principle alter what
  `if` field a `force_down` gateway has at runtime. A testbed apply with
  `:6464::/96` should confirm the route still lands.
- Suburban testbed reachability is currently broken from the workstation
  (`Network unreachable` on `3d06:bad:b01:200::11`). Resolving that path is a
  prerequisite for the testbed runs above.
- This investigation did not exercise `system_routing_configure`, so no fresh
  `ROUTING:` log lines were captured. The interpretation rests on the source
  reading and the prior commits (`bcf4019` operational notes, `4fe72fd`
  Phase 2 force_down recipe) which were authored from live observation of
  the prod cutover sessions.
