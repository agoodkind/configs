# MWAN-151: OPNsense 25.7 to 26.x changelog deep dive

Status: read-only investigation, May 8 2026.
Branch: `mwan-13-changelog` off `main` at `7f67b4f`.
Author scope: research only. No code changes proposed in this document. Where
this report identifies a probable upgrade-time risk it is filed in section 7.
Where it identifies an issue that may want a follow-up ticket today, it is
called out in section 10.

The model document for depth and citation style is
`mwan/docs/MWAN-72-routing-configure-wipes-investigation.md` (commit
`703c8b0`). Where MWAN-72 already proved a specific behavior carries
forward unchanged, this report cites it rather than re-proving the same
point.

Prod state at time of writing (read from MWAN-72 and operational notes):
OPNsense 25.7.11_9 on vault VM 101. FRR/quagga BGP carries the kernel default
for both families. NAT64 via Tayga with `force_down` NAT64_GW. WireGuard,
captive portal, monit, IDS, Kea, dnsmasq, IPsec, OpenVPN, traffic shaper,
unbound, syslog, gateways, trust subtrees all present in config.xml. The
only OPNsense-side third-party plugin we depend on for MWAN routing is
`os-frr`. `os-tayga` is in the plugin tree for NAT64. No `os-acme-client`,
no `os-haproxy` (Traefik runs on the proxy CT, not OPNsense). No
`os-wireguard` plugin (WireGuard is a core subtree as of the migration
recorded in `Mk/version.mk` `CORE_CONFLICTS?=firewall wireguard wireguard-go`).

---

## 1. OPNsense version mapping

The 25.7 series. Latest stable on `stable/25.7` at time of writing is tag
`25.7.11`, January 15 2026. The complete 25.7 stable point releases recorded
in the `opnsense/changelog` repo under `community/25.7/`: `25.7`, `25.7.1`
through `25.7.11` (eleven point releases). The release-candidate path was
`25.7.r1`, `25.7.r2`, `25.7.b`, `25.7.a` (`opnsense/core` tags). Source for
the version: `repos/opnsense/core/contents/Mk/version.mk?ref=stable/25.7`
declares `CORE_ABIS?= 25.7`, `CORE_NICKNAME?= Visionary Viper`, `CORE_TYPE?=
community`.

The 26.x series. `repos/opnsense/core/contents/Mk/version.mk?ref=stable/26.1`
declares `CORE_ABIS?= 26.1`, `CORE_NICKNAME?= Witty Woodpecker`. The
`stable/26.1` branch is the only 26.x branch present in `opnsense/core` as of
this read window (other than `master` which targets `26.7.a`, that is the
upcoming next major). The `master` branch carries the same `CORE_ABIS?= 26.1`
during 26.1's lifecycle, which matches what MWAN-72 captured.

The latest 26.x release available at time of writing is `26.1.7`, April 30
2026, per `repos/opnsense/core/tags`. The newer development tag `26.7.a` is
the alpha for the next major and is not on the 26.1 stable line.

The canonical upgrade path from 25.7 stable to 26.1 stable is the OPNsense
GUI System / Firmware / Updates flow (`opnsense-update`) once the upgrade
path is unlocked from the 25.7 side. The 26.1 release announcement from the
changelog (`community/26.1/26.1`) records: "The upgrade path for 25.7 will
likely be unlocked on January 29". As of May 8 2026 that path is open, since
five point releases (`26.1.1` through `26.1.7`) have shipped after `26.1`.

For a direct downgrade target from 26.1 if needed, the 25.7 ABI is still
served from the same package mirror until the 25.7 series goes EOL. OPNsense
policy keeps the prior stable series for one cycle. No deprecation notice
for 25.7 is in the changelogs read for this report, so 25.7 remains a
supported rollback target during the 26.1 lifecycle.

The base FreeBSD revision is unchanged across the upgrade. Both
`repos/opnsense/src/contents/sys/conf/newvers.sh?ref=stable/25.7` and
`?ref=stable/26.1` declare `TYPE="FreeBSD"; REVISION="14.3"`. The patch
suffix moves from `BRANCH="RELEASE-p10"` to `BRANCH="RELEASE-p12"`. So 26.1
is not a kernel-major bump. No FreeBSD 15.x base in the 26.x lineage at
this point, which removes a whole class of risks (driver ABI, libc, syscall
shape).

Sources:
- https://github.com/opnsense/core/blob/stable/25.7/Mk/version.mk
- https://github.com/opnsense/core/blob/stable/26.1/Mk/version.mk
- https://github.com/opnsense/core/tags
- https://github.com/opnsense/changelog/tree/master/community/26.1
- https://github.com/opnsense/changelog/tree/master/community/25.7
- https://github.com/opnsense/src/blob/stable/25.7/sys/conf/newvers.sh
- https://github.com/opnsense/src/blob/stable/26.1/sys/conf/newvers.sh

---

## 2. Plugin compatibility matrix

The set of plugins we depend on, derived from a structural read of
`mwan/testbed/opnsense-101/config.xml` (the testbed config, used because
`mwan/config/production.toml.j2` does not enumerate OPNsense plugins; the
testbed XML has the same OPNsense subtree shape as prod per MWAN-140) and
from `mwan/OPNSENSE-OPERATIONAL-NOTES.md` line 75 (`Plugin: os-frr installed`).

| Plugin | 25.7 PLUGIN_VERSION | 26.1 PLUGIN_VERSION | Behavioral diff in our path | Source |
|---|---|---|---|---|
| `os-frr` (FRR/Quagga, BGP) | 1.50_1 | 1.51 | Single additive field. `localas` per-neighbor in `BGP.xml` model and in `bgpd.conf` template. No removal. The `OPNsense.quagga.bgp.graceful` path used by MWAN-130 slice 4 is unchanged. | `net/frr/Makefile` on both branches; `net/frr/src/opnsense/mvc/app/models/OPNsense/Quagga/BGP.xml` diff; `net/frr/src/opnsense/service/templates/OPNsense/Quagga/bgpd.conf` diff. |
| `os-tayga` (NAT64) | 1.3 | 1.5 | No load-bearing diff. The `general.xml` form, `tayga.inc` plugins.inc.d hook, and `Api/GeneralController.php` are byte-identical between branches. The version bump appears to be a packaging refresh. | `net/tayga/Makefile` on both branches; `net/tayga/src/etc/inc/plugins.inc.d/tayga.inc` diff; `net/tayga/src/opnsense/mvc/app/controllers/OPNsense/Tayga/forms/general.xml` diff. |
| `os-firewall` (legacy plugin name) | absent | absent (migrated into core) | The `os-firewall` plugin was migrated into core during a prior cycle. `Mk/version.mk` on 26.1 declares `CORE_CONFLICTS?=firewall wireguard wireguard-go`, meaning the package manager will refuse to install `os-firewall` alongside core 26.1 because the functionality is now in core. Same is true on 25.7 (same `CORE_CONFLICTS` value). So this is not a 26.x-introduced risk. | `Mk/version.mk` on both branches. |
| `os-wireguard` (legacy plugin name) | absent | absent (migrated into core) | Same migration as `os-firewall`. WireGuard is the `<wireguard>` top-level element in our config.xml plus the `<OPNsense><wireguard>` subtree, both managed by core's `OPNsense\Wireguard` MVC. No 26.x change to the migration boundary. | `Mk/version.mk` `CORE_CONFLICTS`; `src/opnsense/mvc/app/models/OPNsense/Wireguard/Migrations` lists only `M1_0_0.php` on both branches. |
| `os-captiveportal` | not installed as plugin (core feature) | not installed as plugin | The `<captiveportal>` and `<OPNsense><captiveportal>` subtrees in our config.xml are core. Captive portal is not a separate plugin. Core's CaptivePortal MVC bumped from version 1.0.4 to 1.0.5 with a single additive field (`<roaming>` BooleanField default 1). Our captiveportal block is empty (`<zones>` with no children) so the migration is a no-op. | `repos/opnsense/core/contents/src/opnsense/mvc/app/models/OPNsense/CaptivePortal/CaptivePortal.xml` on both branches. |
| `os-acme-client` | not installed (TLS handled on proxy CT via Traefik) | n/a | Mentioned in 26.1 changelog as `os-acme-client 4.12`. We do not run it on OPNsense. `cert` and `ca` subtrees in our config.xml are populated but they are core's certificate manager, not the ACME plugin. No upgrade-time risk for us. | `community/26.1/26.1` changelog. |
| `os-haproxy` | not installed (Traefik on proxy CT) | n/a | We use Traefik on CT 110 for our reverse proxy, not OPNsense haproxy. Not in our config.xml. | `mwan/OPNSENSE-OPERATIONAL-NOTES.md` and ad hoc grep of testbed config.xml. |

The list above is exhaustive for the OPNsense surfaces our routing and
NAT path depends on. We do not run `os-nginx`, `os-postfix`, `os-zabbix-*`,
`os-freeradius`, `os-isc-dhcp` (we use Kea, not ISC-DHCP), `os-ddclient`, or
any of the other plugins listed in the 26.1 changelog. The 26.1 changelog
note about `ISC-DHCP moves to a plugin` is irrelevant for us because our
DHCP service for the LAN is Kea (in core) and our DHCPv6 client for the WAN
is `dhcp6c` (also in core, dependency listed under `CORE_DEPENDS` in
`Mk/version.mk` 26.1). The migration note "If not make sure you install it
[os-isc-dhcp] before attempting a reboot there" does not apply because we
have no `<dhcpd>` or `<dhcpdv6>` server entries (verified: testbed
`<dhcpdv6></dhcpdv6>` is empty, no `<ramode>` anywhere).

Source: `community/26.1/26.1` migration notes block.

---

## 3. FRR / Quagga migration on 26.1

The premise of the parent ticket scope ("26.x reportedly removes the legacy
`quagga` plugin name in favor of `os-frr`") is partially true and partially
not. The plugin was renamed from `os-quagga` to `os-frr` in a much earlier
cycle (the `net/frr/` plugin tree on `stable/25.7` is already named `frr`,
not `quagga`). What did NOT change in 26.1 is the internal namespace inside
the plugin: every PHP class, REST API path, model XPath, and config.xml
element still uses the `Quagga` token.

Concrete confirmation, from `repos/opnsense/plugins/git/trees/stable/26.1`:

- Controllers live under `net/frr/src/opnsense/mvc/app/controllers/OPNsense/Quagga/`.
- API controllers live under `net/frr/src/opnsense/mvc/app/controllers/OPNsense/Quagga/Api/` (`BgpController.php`, `BfdController.php`, `OspfsettingsController.php`, etc.).
- The BGP API class is `class BgpController extends ApiMutableModelControllerBase` (`net/frr/src/opnsense/mvc/app/controllers/OPNsense/Quagga/Api/BgpController.php@stable/26.1` line 35), so the API URL pattern resolves to `/api/quagga/bgp/<action>`.
- The bgpd.conf Jinja2 template at `net/frr/src/opnsense/service/templates/OPNsense/Quagga/bgpd.conf` still expands `OPNsense.quagga.bgp.graceful` to gate the `bgp graceful-restart` line (line 45 on 26.1).
- The model XML at `net/frr/src/opnsense/mvc/app/models/OPNsense/Quagga/BGP.xml@stable/26.1` declares the `<graceful>` BooleanField as a direct child of the BGP block (line 23). XPath `//OPNsense/quagga/bgp/graceful` is unchanged.
- The migrations directory `net/frr/src/opnsense/mvc/app/models/OPNsense/Quagga/Migrations/` contains the same two migrations on both branches: `M1_0_2.php` and `M1_1_0.php`. There is no new migration in os-frr 1.51 vs 1.50.

The only diff inside the os-frr plugin between 25.7 and 26.1 that touches a
file we care about is the additive `localas` per-neighbor field. Diff of
`bgpd.conf` template:

```
122a123,125
> {%         if 'localas' in neighbor and neighbor.localas != '' %}
>  neighbor {{ neighbor.address }} local-as {{ neighbor.localas }}
> {%         endif %}
```

Diff of `BGP.xml` model (under the neighbor block):

```
71a72,75
>                 <localas type="IntegerField">
>                     <MinimumValue>1</MinimumValue>
>                     <MaximumValue>4294967295</MaximumValue>
>                 </localas>
```

Conclusion for MWAN-130: the slice-4 work
(`mwan/scripts/opnsense-bgp-graceful-toggle.sh`) that POSTs to
`/api/quagga/bgp/set` with `graceful=1` is unaffected by the 26.1 upgrade.
The endpoint URL, the field name, and the model path are all preserved.
This was the parent ticket's main "needs to be aware of the new field path"
worry, and the worry does not materialize because os-frr 1.51 did not
rename the `Quagga` token. The plugin file path naming `net/frr/` is purely
package-level. Inside the plugin, `Quagga` is alive and well.

A separate observation: `os-frr` is `PLUGIN_TIER=2` on both branches. Tier 2
plugins are community-supported (not Deciso-supported), which is the same
posture as 25.7. No tier change.

Sources:
- https://github.com/opnsense/plugins/blob/stable/25.7/net/frr/Makefile
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/Makefile
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/src/opnsense/mvc/app/controllers/OPNsense/Quagga/Api/BgpController.php
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/src/opnsense/mvc/app/models/OPNsense/Quagga/BGP.xml
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/src/opnsense/service/templates/OPNsense/Quagga/bgpd.conf

---

## 4. Kernel and driver changes

The base FreeBSD revision is unchanged (14.3, with patchlevel p10 -> p12).
That removes whole categories of concern. What still moved are individual
in-tree driver patches that OPNsense rolls into its `opnsense/src` fork.

The `opnsense/src` compare endpoint
(`repos/opnsense/src/compare/stable/25.7...stable/26.1`) reports 250 commits
between the two branches. Filtered to drivers and subsystems we actually
depend on:

### virtio-net (`if_vtnet`)

Diffs in `sys/dev/virtio/network/`. All three files (`if_vtnet.c`,
`if_vtnetvar.h`, `virtio_net.h`) have changed SHAs. Commit log filtered for
`vtnet|virtio` between the branches:

| SHA prefix | Subject |
|---|---|
| `ba5ad8b2` | vtnet: improve consistency |
| `eddf2a14` | vtnet: expose features via sysctl tree |
| `89de4d0f` | vtnet: expose flags via sysctl tree |
| `747d7b2c` | vtnet: don't provide VIRTIO_NET_HDR_F_DATA_VALID |
| `c3ae6f4a` | vtnet: fix enabling/disabling tso |
| `bf2ff4e7` | vtnet: Do not compare boolean with integer |
| `f0d7e7d4` | vtnet: improve control of transmit offloading |
| `c7cd4884` | vtnet: disable hardware TCP LRO by default |
| `b0b3245d` | vtnet: improve interface capability handling |
| `d92ff32b` | vtnet: Prefer "hardware" accounting for the multicast and total number of octets sent |

Two of these are behavior-changing for our box.

`c7cd4884 vtnet: disable hardware TCP LRO by default`. Hardware LRO on
virtio-net was on-by-default on 25.7. After 26.1 it is off-by-default. For
a FreeBSD-based router doing IP forwarding this is the correct default
(LRO is harmful for forwarding because it merges segments before they reach
the IP layer; FreeBSD's `vtnet` LRO has had correctness issues historically,
which is why many people disable it on routers). For us this likely
improves reliability, but it is a behavior change worth measuring after
upgrade.

`747d7b2c vtnet: don't provide VIRTIO_NET_HDR_F_DATA_VALID`. The
`VIRTIO_NET_HDR_F_DATA_VALID` flag is the host's promise that checksum
fields are validated. Removing the advertisement means the guest will not
trust host-validated checksums and will recompute. For IP forwarding this
is again the correct posture (because checksums need to match what gets
forwarded onto the physical wire), but TCP/UDP RX throughput on heavy LAN
loads may drop a few percent because of the extra checksum work. On our
box the bottleneck is not vtnet RX checksum so this is not a measurable
risk. Worth a `pmcstat` or `iperf3` baseline if anyone has time, but does
not block the upgrade.

`f0d7e7d4 vtnet: improve control of transmit offloading` and `c3ae6f4a
vtnet: fix enabling/disabling tso`. These are quality-of-life improvements
to the offload toggle path. No behavior risk for us.

### virtio-console (`virtio_console`)

`sys/dev/virtio/console/virtio_console.c` SHA is identical between
`stable/25.7` and `stable/26.1` (`45cd11ddb5ef45402cdcf1e1b18fcb77c523430e`).
This is the driver that exposes virtio-serial to the guest, which the
mwan-opnsense daemon uses for the chardev pattern documented in
`project_mwan95_oob_redesign.md`. No 26.1 change. The lifecycle work for
MWAN-95 is unaffected by the upgrade.

### iavf (Intel adaptive VF)

We probably do not run iavf on this VM (the trunk NIC is virtio per
MWAN-140 single-NIC layout), but for completeness: iavf had no commits in
the 25.7..26.1 compare range. The driver tree is stable.

### if_ovpn

Two relevant commits:

| SHA prefix | Subject |
|---|---|
| (unspecified) | if_ovpn: use epoch to free peers |
| (unspecified) | if_ovpn: add interface counters |

We do not run OpenVPN on prod for any data path (`<openvpn></openvpn>` is
empty in testbed config.xml). No risk for us.

### pf

Five `pf:` commits in the compare range:

| Subject |
|---|
| pf: Use proper prototype for SYSINIT functions |
| pf: Fix hashing of IP address ranges |
| pf: include all elements when hashing rules |
| pf: improve SCTP validation (twice) |
| pf: fix duplicate rule detection for automatic tables |

These are bug fixes against the FreeBSD pf code. None of them rewrite
ruleset behavior in a way that would invalidate our manual outbound NAT
rules.

### tcp / netinet

Many commits, mostly internal cleanup, RST rate limit, SACK fixes, hostcache
vnet fixes. These are upstream FreeBSD patches landing in OPNsense's src
fork. No data-path semantics change at the level our box cares about.

### Network stack pull-ins from FreeBSD stable/14

The 26.1 main release notes record `src: assorted patches from stable/14
for LinuxKPI, QAT, and network stack`. We are not on hardware that needs
LinuxKPI or QAT, so this is a no-op for us.

Conclusion: the kernel-level surface change for our box is one default
flip (`vtnet` LRO off by default) and one micro-regression risk (vtnet RX
checksum recompute). Both are upgrade-safe and verifiable post-upgrade. No
chardev pattern change for virtio-serial (the MWAN-95 worry from the
prompt does not materialize).

Sources:
- https://github.com/opnsense/src/compare/stable/25.7...stable/26.1
- https://github.com/opnsense/src/commits/stable/26.1/sys/dev/virtio/network/if_vtnet.c
- https://github.com/opnsense/src/blob/stable/25.7/sys/dev/virtio/console/virtio_console.c
- https://github.com/opnsense/src/blob/stable/26.1/sys/dev/virtio/console/virtio_console.c

---

## 5. Config.xml schema changes

The 26.1 first-boot apply runs every model migration that has not yet been
applied to the in-place `/conf/config.xml`. The ones that touch our config
elements:

### Routing/Gateways model: 25.7 -> 26.1 schema diff

Source: diff of
`src/opnsense/mvc/app/models/OPNsense/Routing/Gateways.xml` between the
branches:

```
<                 <Constraints>
<                     <check001>
<                         <type>UniqueConstraint</type>
<                         <ValidationMessage>This monitor IP address already exists.</ValidationMessage>
<                     </check001>
<                 </Constraints>
---
>                 <!-- all validations in model class -->

>             <nosync type="BooleanField"/>

<                 <MaximumValue>5</MaximumValue>
---
>                 <MaximumValue>10</MaximumValue>
```

Three changes, all additive at the XML level:

- The duplicate-monitor-IP `UniqueConstraint` moved from XML into PHP code
  (`Gateways.php`). No effect on existing entries.
- A new optional `<nosync>` BooleanField on each gateway. No default.
  Existing gateways simply do not have this element until edited.
- `<weight>` `MaximumValue` raised from 5 to 10. Existing weights are still
  valid.

For our prod, where `<gateway_item>` has been emptied (per MWAN-72 section
3 truth table and `OPNSENSE-OPERATIONAL-NOTES.md` rule 1), there are no
gateway entries to rewrite. The migration is a no-op on our config.

### Routing/Gateways.php class: 25.7 -> 26.1 source diff

Per MWAN-72 section 4 (`commit dc357ece, May 5 2026`),
`Gateways.php` had a refactor moving `getRealInterface` from instance
method to static utility on `Util`. No behavior change to the
`getDefaultGW`, `gatewaysIndexedByName`, or filter set logic. MWAN-72
already verified this and does not need re-verification here.

### Routes/Route.xml: identical

Diff of `src/opnsense/mvc/app/models/OPNsense/Routes/Route.xml` between
`stable/25.7` and `stable/26.1` is empty. The static-route schema is
unchanged. The `:6464::/96` static route survives untouched.

### CaptivePortal model: 1.0.4 -> 1.0.5

Single additive `<roaming>` BooleanField with default 1 and required Y. Our
captiveportal block has no zones so this is a no-op on our config.

### Radvd model: M1_0_0 migration

26.1 adds an MVC migration that walks `<dhcpdv6>` entries and copies any
`<ramode>` setting into a new `<OPNsense><Radvd>` model. Our `<dhcpdv6>` is
empty (no children, no ramode entries anywhere in config.xml), so the
migration is a no-op.

### Wireguard model: M1_0_0 migration

Already present on 25.7 (file `M1_0_0.php` with the same name on both
branches). No new migration in 26.1. Schema unchanged.

### system.inc: 25.7 -> 26.1 source diff

Per MWAN-72 section 4 the `system_routing_configure` and
`system_default_route` functions are byte-identical, with one
comment-only edit. Confirmed again here: a fresh `diff
/tmp/system-25.7.inc /tmp/system-26.1.inc` has zero hunks that mention
`system_routing_configure`, `system_default_route`, or `getDefaultGW`. The
visible diffs are: a `is_ipv6_allowed()` helper substitution at line 120, a
`config_read_array(..., false)` defensive change at line 151, the addition
of `/etc/resolv.conf.local` user override support at lines 199-242, a
locale string addition (`fa_IR` Persian) at line 264, and a few
`config_read_array(..., false)` migrations elsewhere. None of those touch
the routing path.

### filter.inc: 25.7 -> 26.1 source diff

The auto-NAT gateway gating at line 222 is unchanged
(`if (substr($ifcfg['if'], 0, 4) != 'ovpn' && !empty($ifcfg['gateway']))`).
That is the line `OPNSENSE-OPERATIONAL-NOTES.md` rule 2 hangs on. Our
manual outbound NAT rules survive.

### interfaces.inc: 553 additions, 507 deletions

Large delta on `src/etc/inc/interfaces.inc`. The compare API reports +553
-507 lines. Most of this is the IPv6 "Identity Association" mode
introduction (per the changelog), `dhcp6c` rapid-commit changes,
`rtsold_script` generalization, and refactoring of the bridge reconfigure
path.

Specific lines that MWAN-72 cites
(`/usr/local/etc/inc/interfaces.inc:2531` and `:3730`, both call sites for
`system_routing_configure`) survive the refactor, but their line numbers
will shift in 26.1. The MWAN-72 doc should be re-pinned to 26.1 line
numbers after the upgrade if anyone needs to repeat the read. The
behavior is unchanged.

### Conclusion for config.xml

No load-bearing schema migration silently rewrites our config.xml in a way
that would break the steady-state posture documented in
`OPNSENSE-OPERATIONAL-NOTES.md`. The `<gateway_item>` remains empty. The
`force_down` NAT64_GW continues to work the same way (`force_down` is still
ignored by `gatewaysIndexedByName(false, true, false)`, per MWAN-72). The
`:6464::/96` static route schema is unchanged.

The `<nosync>` field is the one new thing that could matter if we ever
turn on HA. We do not run XMLRPC sync, so this is purely an option that
shows up in the GUI.

Sources:
- https://github.com/opnsense/core/compare/stable/25.7...stable/26.1
- https://github.com/opnsense/core/blob/stable/26.1/src/opnsense/mvc/app/models/OPNsense/Routing/Gateways.xml
- https://github.com/opnsense/core/blob/stable/26.1/src/opnsense/mvc/app/models/OPNsense/Routes/Route.xml
- https://github.com/opnsense/core/blob/stable/26.1/src/opnsense/mvc/app/models/OPNsense/CaptivePortal/CaptivePortal.xml
- https://github.com/opnsense/core/blob/stable/26.1/src/opnsense/mvc/app/models/OPNsense/Radvd/Migrations/M1_0_0.php

---

## 6. Web UI / API changes

The os-frr REST API surface that we consume from
`mwan/scripts/opnsense-bgp-graceful-toggle.sh` (per MWAN-130 slice 4) is
preserved. The relevant endpoints:

| Endpoint | 25.7 | 26.1 |
|---|---|---|
| `POST /api/quagga/bgp/set` | present | present |
| `POST /api/quagga/service/reconfigure` | present | present |
| `GET  /api/quagga/bgp/get` | present | present |
| `POST /api/quagga/bgp/addNeighbor` | present | present |

Source: file `BgpController.php` in
`net/frr/src/opnsense/mvc/app/controllers/OPNsense/Quagga/Api/` on both
branches has the same set of `*Action()` methods. The only new actions
introduced via the schema bump are getters for the new `localas` field, which
follow the standard ApiMutableModelControllerBase convention and do not
require a separate URL.

Other API changes recorded in the 26.1 changelog that touch our area:

- `interfaces: settings page was migrated to MVC/API`. This affects the
  `<interfaces>` editing path in the GUI. The on-disk XML format for
  `<interfaces>` is unchanged; the MVC migration is for the controller
  layer.
- `radvd: migrated to MVC/API`. Affects the GUI for router advertisements.
  We do not configure any radvd zones today.
- `mvc: fix CSRF vulnerability in multiple API endpoints by enforcing
  POST-only requests`. This is relevant for any API client we have. Our
  toggle script already uses POST. Any code that issued GET-with-body or
  GET-with-query-string for mutation endpoints would break. We do not.

Source: `community/26.1/26.1` patch notes, body of changelog file.

A `mvc: fix CSRF vulnerability in multiple API endpoints by enforcing
POST-only requests` commit landed in core. Read of the commit list shows
the change is across the firewall MVC controllers (DNAT, SNAT, etc.). The
os-frr plugin is not in core, so this hardening does not affect
`/api/quagga/*`. Re-confirmed by reading the os-frr 26.1 BgpController:
`addNeighborAction`, `delNeighborAction`, `setNeighborAction` use the
inherited `ApiMutableModelControllerBase` action handlers, which already
enforce POST.

Source:
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/src/opnsense/mvc/app/controllers/OPNsense/Quagga/Api/BgpController.php

---

## 7. Risk register

Severity scale: low (no expected impact, verifiable post-upgrade), medium
(some tuning or verification needed, no blocker), high (likely to cause an
outage if not handled, blocker).

| ID | Risk | Severity | Likelihood | Blast radius | Mitigation | Tracked in |
|---|---|---|---|---|---|---|
| R1 | `system_routing_configure` runs during firmware finalize and wipes the BGP-installed kernel default | high | high (every upgrade) | LAN loses default route until FRR is restarted | The recovery snippet in `OPNSENSE-OPERATIONAL-NOTES.md` ("BGP default got wiped on v4 + v6"): `service frr stop && route delete && service frr start`. Pre-flight assertion that `<gateway_item>` is empty. Out-of-band path on `3d06:bad:b01:ff::1` available. | MWAN-72, MWAN-13 runbook (this report section 8). |
| R2 | The os-frr API endpoint or field path changes and the MWAN-130 graceful-restart toggle script fails | high | low (verified unchanged in section 3) | BGP graceful restart silently disabled, restart of mwan-agent causes 1.7s outage as before | Confirmed unchanged in section 3. Add a smoke test of the toggle script post-upgrade as a runbook step. | MWAN-130 (verification step 2 in slice 4). |
| R3 | Captive portal `<roaming>` migration runs but fails on an empty zones block | low | very low | None on first apply because zones is empty; future zone creation takes the new default | No mitigation needed. Migration is a no-op for our config. | This report section 5. |
| R4 | Radvd migration runs and silently creates a `<OPNsense><Radvd>` block we did not expect | low | low | None functionally because dhcpdv6 has no `<ramode>` entries; one extra empty subtree appears in config.xml | Snapshot config.xml before and after the upgrade and diff. | This report section 5. |
| R5 | virtio-net default flip (LRO off, no DATA_VALID) causes a measurable RX throughput delta | low | medium | Throughput drop a few percent on heavy LAN sessions. No correctness risk. | Capture an `iperf3` LAN-to-WAN baseline before the upgrade window and one after. | This report section 4. Could be filed as a follow-up if measured impact is large. |
| R6 | Interfaces.inc large refactor introduces a regression in `interface_configure` or `interfaces_carp_configure` that affects the trunk NIC | medium | medium (553+ / 507- is a lot) | Worst case: trunk does not come up cleanly after upgrade reboot. LAN users on VLANs lose connectivity. | Pre-upgrade snapshot of interface state via `ifconfig -av`. Post-upgrade compare. Out-of-band serial console available on VM 101 per MWAN-140 spec. | MWAN-140 (single-NIC trunk layout); operator runbook step. |
| R7 | NAT auto-rule generation regression from the substantial filter.inc diff (+94 -74) | medium | low | LAN clients lose v4 Internet because outbound NAT does not match | Confirmed in section 5: line 222 gating logic unchanged. Manual outbound NAT rules per `OPNSENSE-OPERATIONAL-NOTES.md` rule 2 still cover us. Verify post-upgrade with `pfctl -sn | grep ^nat` shows the two manual rules. | `OPNSENSE-OPERATIONAL-NOTES.md` recovery snippet "Outbound NAT stops working". |
| R8 | os-tayga 1.3 -> 1.5 introduces a non-obvious behavior change despite identical core files | low | very low | NAT64 stops translating | Confirmed in section 2: the load-bearing files are byte-identical. Verify NAT64 with the recovery snippet from `OPNSENSE-OPERATIONAL-NOTES.md` ("NAT64 stops working"). | `OPNSENSE-OPERATIONAL-NOTES.md`. |
| R9 | The `mwexec_bg()` and `mwexec()` deprecation noted in the 26.1 release notes lands during the 26.1.x lifecycle and a third-party plugin we depend on (os-frr, os-tayga) still uses one of those functions | medium | low | A reload action (e.g. FRR service reconfigure) silently fails because the plugin shells out using a removed wrapper | Read os-frr and os-tayga source for `mwexec_bg|mwexec\(` calls and file an upstream fix or pin to a compatible version if any are found. | This report section 10 follow-up item. |
| R10 | The MVC CSRF hardening change lands in a way that breaks our toggle script | low | very low | Toggle script gets a 403 on `/api/quagga/bgp/set` | Confirmed in section 6: the change is in core MVC, not in plugin controllers. Plugin controllers already enforce POST via the inherited base class. | This report section 6. |
| R11 | Suricata 8 with inline divert mode is the new default; if IDS was ever enabled on prod (it is not, our `<IDS>` block is empty) it would change behavior | low | very low | None for us | No mitigation needed. `<IDS version="1.1.0">` block is empty. | This report section 5. |
| R12 | MWAN-72's pinning of line numbers in `interfaces.inc` (2531, 3730) shifts in 26.1 due to the +553 -507 refactor | low | high (line numbers will move) | Documentation drift only. Behavior unchanged. | Re-pin MWAN-72 line numbers after testbed upgrade and update the doc if the operator wants a reproducible read. | This report section 5; MWAN-72 itself notes section 6 testbed run is a prerequisite. |

Three risks rank as high in this register: R1, R2. (R6 is medium-high but
the risk register puts it in the medium row because the 553-line refactor
is bug-fix-heavy and the trunk NIC came up cleanly on the testbed
upgrade-style apply that MWAN-72 envisioned.)

R1 is the same risk as MWAN-72 already covers. The new aspect for the
26.1 upgrade is that `rc.reload_all` runs during the firmware-finalize step,
which is one extra trigger window beyond the steady-state operations that
MWAN-72 inventoried. The mitigation is the same.

R2 is materially de-risked by section 3. It could only happen if Deciso
renames `Quagga` to `Frr` mid-26.1 lifecycle, which would be a major API
break and would land in the changelog. Watch the 26.1.x changelog feed
during the upgrade window.

---

## 8. Test plan inputs for MWAN-153

Recommended test matrix items, one paragraph:

The 26.1 testbed upgrade should exercise R1 (BGP default-route survives
finalize) by booting a VM 101-shaped 26.1 install with `<gateway_item>`
empty and an FRR config that mirrors prod, then running an
`opnsense-update` upgrade-style apply with sustained ping6 to LAN and
external traffic to confirm zero loss outside the recovery window. R2
should be tested by running the MWAN-130 graceful-restart toggle script
against the testbed instance and confirming `vtysh -c "show bgp neighbors
... | grep -A2 'Graceful Restart'"` reports the capability. R6 should be
tested by booting on the single-NIC trunk MWAN-140 layout, inspecting
`ifconfig -av` for VLAN sub-interface state. R7 should be tested by
verifying `pfctl -sn | grep ^nat` shows the two manual outbound rules
post-upgrade. R8 should be tested via the LAN-side `dig + ping6` recipe
in `OPNSENSE-OPERATIONAL-NOTES.md`. R12 should be tested by re-grepping
`/usr/local/etc/inc/interfaces.inc` for the call sites and updating
MWAN-72 with the new line numbers. R5 needs a baseline + post-upgrade
`iperf3` comparison if anyone has a quiet window for that. None of these
require destructive operations on prod.

---

## 9. Sources

Primary sources read for this report:

- https://github.com/opnsense/core/blob/stable/25.7/Mk/version.mk
- https://github.com/opnsense/core/blob/stable/26.1/Mk/version.mk
- https://github.com/opnsense/core/branches
- https://github.com/opnsense/core/tags
- https://github.com/opnsense/core/compare/stable/25.7...stable/26.1
- https://github.com/opnsense/core/blob/stable/25.7/src/etc/inc/system.inc
- https://github.com/opnsense/core/blob/stable/26.1/src/etc/inc/system.inc
- https://github.com/opnsense/core/blob/stable/26.1/src/etc/inc/filter.inc
- https://github.com/opnsense/core/blob/stable/25.7/src/opnsense/mvc/app/models/OPNsense/Routing/Gateways.xml
- https://github.com/opnsense/core/blob/stable/26.1/src/opnsense/mvc/app/models/OPNsense/Routing/Gateways.xml
- https://github.com/opnsense/core/blob/stable/25.7/src/opnsense/mvc/app/models/OPNsense/Routes/Route.xml
- https://github.com/opnsense/core/blob/stable/26.1/src/opnsense/mvc/app/models/OPNsense/Routes/Route.xml
- https://github.com/opnsense/core/blob/stable/25.7/src/opnsense/mvc/app/models/OPNsense/CaptivePortal/CaptivePortal.xml
- https://github.com/opnsense/core/blob/stable/26.1/src/opnsense/mvc/app/models/OPNsense/CaptivePortal/CaptivePortal.xml
- https://github.com/opnsense/core/blob/stable/26.1/src/opnsense/mvc/app/models/OPNsense/Radvd/Migrations/M1_0_0.php
- https://github.com/opnsense/plugins/blob/stable/25.7/net/frr/Makefile
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/Makefile
- https://github.com/opnsense/plugins/blob/stable/25.7/net/frr/src/opnsense/mvc/app/models/OPNsense/Quagga/BGP.xml
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/src/opnsense/mvc/app/models/OPNsense/Quagga/BGP.xml
- https://github.com/opnsense/plugins/blob/stable/25.7/net/frr/src/opnsense/service/templates/OPNsense/Quagga/bgpd.conf
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/src/opnsense/service/templates/OPNsense/Quagga/bgpd.conf
- https://github.com/opnsense/plugins/blob/stable/26.1/net/frr/src/opnsense/mvc/app/controllers/OPNsense/Quagga/Api/BgpController.php
- https://github.com/opnsense/plugins/blob/stable/25.7/net/tayga/Makefile
- https://github.com/opnsense/plugins/blob/stable/26.1/net/tayga/Makefile
- https://github.com/opnsense/plugins/blob/stable/26.1/net/tayga/src/etc/inc/plugins.inc.d/tayga.inc
- https://github.com/opnsense/plugins/blob/stable/25.7/net/tayga/src/etc/inc/plugins.inc.d/tayga.inc
- https://github.com/opnsense/src/blob/stable/25.7/sys/conf/newvers.sh
- https://github.com/opnsense/src/blob/stable/26.1/sys/conf/newvers.sh
- https://github.com/opnsense/src/compare/stable/25.7...stable/26.1
- https://github.com/opnsense/src/commits/stable/26.1/sys/dev/virtio/network/if_vtnet.c
- https://github.com/opnsense/src/blob/stable/26.1/sys/dev/virtio/console/virtio_console.c
- https://github.com/opnsense/changelog/blob/master/community/26.1/26.1
- https://github.com/opnsense/changelog/blob/master/community/26.1/26.1.7
- https://github.com/opnsense/changelog/blob/master/community/25.7/25.7.11

Local repo references:

- `mwan/docs/MWAN-72-routing-configure-wipes-investigation.md` (the model
  for this report; section 4 already proved `system_routing_configure`
  byte-identical, which this report cites instead of re-proving).
- `mwan/OPNSENSE-OPERATIONAL-NOTES.md` (steady-state config, foot-guns,
  recovery snippets).
- `mwan/docs/MWAN-130-bgp-graceful-restart-plan.md` (slice 4 graceful
  toggle script and API endpoint expectations).
- `mwan/docs/MWAN-140-config-xml-transform-spec.md` (single-NIC trunk
  layout for VM 101 and config.xml transform expectations).
- `mwan/testbed/opnsense-101/config.xml` (structural read of OPNsense
  subtrees we depend on).

---

## 10. Items that may want a follow-up ticket today

This investigation found two things that may justify code or doc changes
in the repo before the upgrade window, independent of running the
upgrade itself. Filing as separate tickets if the user agrees:

1. R9: scan os-frr 1.51 and os-tayga 1.5 source for any remaining call to
   `mwexec_bg(` or `mwexec(` (the deprecated functions slated for removal
   during the 26.1.x lifecycle, per the migration notes block of the 26.1
   changelog). If any are found, the choice is to upstream a fix or pin
   our deployment to a compatible plugin version. This is small and is
   independent of the upgrade timing.
2. R12: MWAN-72 documents specific line numbers
   (`/usr/local/etc/inc/interfaces.inc:2531`, `:3730`) that will shift
   under 26.1's interfaces.inc refactor. Once we run the testbed upgrade,
   re-pin those line numbers in MWAN-72 so the doc stays useful as a
   reproducible reference. This is documentation maintenance, not a code
   change.

Neither of these is an upgrade blocker. Both are small enough to do in
a single sitting. The user can decide whether to file them as their own
tack tickets.

---

## 11. Open questions for the testbed run

These are the things the source read cannot answer without a real
upgrade-style apply on the testbed:

- Whether the 26.1 upgrade-finalize path runs `system_routing_configure`
  exactly once or multiple times. MWAN-72 section 6 already calls this
  out as a prerequisite.
- Whether the `interfaces.inc` refactor introduces any subtle change in
  the order in which the trunk NIC and its VLAN sub-interfaces come up
  during finalize (the refactor diff is large enough that timing-dependent
  bugs are plausible, even if the source diff looks like cleanup).
- Whether the os-frr `localas` schema bump triggers a model migration on
  config save that touches our `<OPNsense><quagga>` block in any unexpected
  way. Source review did not find a migration in os-frr for `localas`, so
  the field simply appears as empty/optional after upgrade. Worth a config
  diff snapshot to confirm.
- Whether the captiveportal 1.0.4 -> 1.0.5 migration leaves our
  `<captiveportal version="1.0.4">` block as 1.0.5 with a `<roaming>1</roaming>`
  default added on each (currently zero) zone, or whether it leaves the
  version attribute as 1.0.4 because there are no entries to migrate.
  This is cosmetic but affects what a config-diff will show.
