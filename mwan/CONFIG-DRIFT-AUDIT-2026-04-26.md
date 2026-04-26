# MWAN Config Template Drift Audit (2026-04-26)

Comprehensive sweep comparing prod vs testbed configuration sources.

## Severity legend

| Tag | Meaning |
|-----|---------|
| BLOCKER | Will cause prod cutover to fail |
| HIGH | Inconsistency that masks real bugs |
| MEDIUM | Drift that requires manual sync, will silently rot |
| LOW | Cosmetic or dead-code |

---

## BLOCKER 1: `firewall_source_v4` and `firewall_source_v6` missing in prod template

`mwan/config/production.toml.j2` is missing both lines under `[opnsense.bgp]`:

```
firewall_source_v4 = "{{ opnsense_bgp_firewall_source_v4 }}"
firewall_source_v6 = "{{ opnsense_bgp_firewall_source_v6 }}"
```

The vars exist in `mwan_servers.yml` (lines 335-336) but were never wired into the template. Effect: `mwan cutover2 configure-opnsense` on prod fails on `addRule`:
`validations=map[rule.source_net:A value is required.]`

The testbed template has both lines. Single-source drift.

**Fix:** add both lines to prod template under `[opnsense.bgp]`.

## HIGH 2: `service_name` was missing from both templates (fixed today, commit `11dd67a`)

The watchdog `service_name` field was added to live config on suburban manually but never to the template. Re-rendering wiped it and `switch-to-bgp` targeted `mwan-watchdog` instead of `mwan-watchdog-testbed`. Same class as Blocker 1: live-only config that template renders silently strip.

**Already fixed** in commit `11dd67a` for both templates.

## HIGH 3: Watchdog `.env` files are largely dead code, prod uses Jinja, testbed uses hardcoded

Two completely different files:
- `mwan/proxmox/config/mwan-watchdog.env.j2` (prod, Jinja-templated)
- `mwan/testbed/suburban-watchdog.env.j2` (testbed, hardcoded values)

Both are loaded via `EnvironmentFile=` by the systemd unit. But the new Go binary only reads three env vars:
- `SMTP2GO_API_KEY`
- `PVE_TOKEN_SECRET`
- `OPNSENSE_API_SECRET`

Everything else in those env files is ignored. The whole layer survives from the bash watchdog era. Anything we add to prod we must remember to also add (hardcoded) to testbed, and almost none of it actually does anything anymore.

**Recommended fix:** delete both .env files from the systemd EnvironmentFile, move the three secrets into Ansible-managed drop-ins or rely on toml. Strip the dead vars.

## MEDIUM 4: Testbed has explicit `interfaces` static route on LXC 100, prod LXC 116 relies on RA

Testbed `mwan/testbed/lxc-100/interfaces` has:
```
post-up ip -6 route replace 3d06:bad:b01:211::/64 via 3d06:bad:b01:201::2 dev eth1
```

This is the IPv6 failover return-routing fix. On prod LXC 116 there is no equivalent file. Prod LXC 116 instead has an RA-learned route `3d06:bad:b01::/60 via fe80::...` covering the same prefix space.

That works as long as RA from VM 113 keeps arriving. If keepalived on VM 113 stops sending VRRP RAs (e.g. it gets removed in cutover2 phase 5), or if RA times out, prod LXC 116 loses its return path silently. Different code path than the one we validated on testbed.

**Fix:** add a static post-up route on prod LXC 116 that mirrors the testbed approach but uses prod prefixes.

## MEDIUM 5: Testbed LXC 100 has dead `frr.conf` and `daemons` files

`mwan/testbed/lxc-100/` ships `frr.conf` and `daemons`, but FRR is `inactive` on the live LXC. We use embedded GoBGP via `mwan-agent`. The repo files are leftovers from a pre-mwan-agent design.

**Fix:** delete `frr.conf` and `daemons` from `mwan/testbed/lxc-100/`.

## MEDIUM 6: networkd templates split between prod and testbed with intentional differences

`mwan/networkd/*.j2` has 10 files. `mwan/networkd/testbed/*.j2` has only 4 of those, and they are intentionally different:

- prod `20-att.link.j2` matches by `Driver=iavf` (PCI passthrough) and spoofs MAC for EAP
- testbed `20-att.link.j2` matches by `MACAddress=` (virtio) and skips EAP
- similar story for webpass

Each pair is a near-twin that diverges on environmental details. There is no mechanism that protects future edits to the prod one from being forgotten on the testbed one. Working as intended for environmental reasons, but high-touch.

**No fix recommended:** the differences are semantically required. Worth a comment in each prod template pointing at its testbed counterpart so an editor sees both.

## MEDIUM 7: Three wholly separate watchdog systemd unit files

| File | Where used |
|------|-----|
| `mwan/proxmox/services/mwan-watchdog.service.j2` | prod (Jinja) |
| `mwan/testbed/mwan-watchdog-testbed.service` | testbed (static) |

Same pattern, two files. If anyone changes one, the other is silent and stale.

**Recommended fix:** consolidate to one Jinja template with a service-name var and a `WantedBy` var.

## LOW 8: `mwan/proxmox/` and `mwan/testbed/` directory structures are not analogous

Prod side has `proxmox/{config,scripts,services}/`. Testbed has `testbed/{cloudflare,cutover,isp-lxc,lxc-100,opnsense-101,vm-950}/` plus a pile of one-off `.sh`/`.toml`/`.conf` files at the root. Even where the underlying purposes are similar (watchdog, networkd, helper scripts), there is no path symmetry.

**Recommended fix (long term):** unify under a single `mwan/hosts/{prod,testbed}/{role}/` layout. Big refactor, not urgent.

## LOW 9: `mwan_systemd_check_interval` referenced in `mwan.env.j2`, defined nowhere

Renders via `default()` filter so it is harmless. Either define it explicitly in both group_vars files or remove the reference.

## Variables defined but never referenced

Survey ran clean. All top-level vars in `mwan_servers.yml` and `mwan_testbed_servers.yml` are reachable from at least one template.

---

## Recommended remediation order

1. **Fix Blocker 1** (firewall_source in prod template). Until this lands, prod cutover is impossible.
2. **Fix Medium 4** (LXC 116 static return route) before any production cutover. Removes a hidden dependency on RA stability.
3. Address High 3 (env file consolidation) in a follow-up cleanup commit.
4. Address Medium 5 (delete dead testbed FRR files) opportunistically.
5. Address Medium 6 / Medium 7 / Low 8 / Low 9 in a refactor pass when time permits.
