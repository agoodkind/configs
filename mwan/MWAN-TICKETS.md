# MWAN ticket list

**Source**: Tack workspace `main`, project `MWAN` (org `019dc5ad-0408-7e43-9c4d-d3e6736ac058`).
**Snapshot taken**: 2026-04-25 ~21:00 UTC after reconciliation sweep.

## Current totals (MWAN)

- **46 issues** (sequences 1-46)
- **20 done**, **26 open**

## Workspace totals (for context)

| Project | Total |
|---|---|
| TACK | 160 |
| CLYDE | 93 |
| **MWAN** | **46** |
| LAB | 33 |
| APP | 17 |
| OSS | 14 |
| WEBSITE | 12 |
| **Total** | **375** |

## Reconciliation pass (this session)

Marked **17 issues as `done`** based on work shipped in past sessions or earlier today. Created **5 new issues** for findings from today (2 fixes shipped, 1 doc-only finding, 2 open follow-ups).

### Marked done

| # | Title | Why now |
|---|---|---|
| MWAN-2 | Test virtual cord yank on testbed mirroring prod behavior | `qm stop 950` test today: ~7.4s gap, IPv4 0% post-recovery, IPv6 0% post-recovery |
| MWAN-23 | Ansible playbook for testbed deployment | Shipped 2026-04-12 (`deploy-mwan-testbed.yml`) |
| MWAN-24 | NPT prefix discovery (remove hardcoded PDs) | Shipped 2026-04-12 (`find-pd-prefixes.sh` + truncate-to-/60) |
| MWAN-25 | Testbed production parity | Done in 2026-04-12 sessions |
| MWAN-26 | OPNsense reply-to disabled on prod and testbed | Done in 2026-04-12 sessions |
| MWAN-27 | Cutover2 testbed validation: 3 runs + failover/failback | Plus runs 4+5 today |
| MWAN-28 | mwan health-check subcommand | Shipped 2026-04-12 |
| MWAN-29 | Production nftables on-disk fix (vrrp.* in forward chain) | Shipped 2026-04-12 |
| MWAN-30 | BLOCKER: Add TCP 179 (BGP) to nftables input chain | In template; deployed |
| MWAN-31 | BLOCKER: Verify IPv6 BGP next-hop | Added `next_hop_v6` config field; deployed |
| MWAN-32 | BLOCKER: Auto-rollback threshold too tight | Raised to 45s + pause/resume during FRR restart |
| MWAN-33 | BLOCKER: Add production BGP / OPNsense API variables | `mwan_servers.yml` populated, vault keys added |
| MWAN-34 | route-to references .1 after BGP cutover | `pf_disable_force_gw` checked on prod and testbed |
| MWAN-35 | Stop production watchdog before cutover2 | Wired into `switch-to-bgp` and `unfuck` |
| MWAN-36 | Unfuck circular dependency | Switched to `qm guest exec` instead of SSH |
| MWAN-39 | Verify and deploy LXC 116 | Deployed 2026-04-12; `lxc-116/` config present |
| MWAN-41 | IPv6 gatewayv6 removal via Go encoding/xml | `mwan/go/internal/cutover2/opnsense_config.go` |

### New tickets (this session)

| # | Title | Priority | State |
|---|---|---|---|
| MWAN-42 | NPT boot script: log warnings to stderr so fallback prefix isn't poisoned | high | done |
| MWAN-43 | Dispatcher hook 55-update-npt.sh: accept IFACE/STATE from env vars | high | done |
| MWAN-44 | networkd-dispatcher: enable INFO logging to surface hook invocations in journal | low | open |
| MWAN-45 | Webpass drops ICMPv6 echo with payload <= 8 bytes (environmental, documented) | low | done |
| MWAN-46 | Monkeybrains DHCPv6-PD lease silently lapses ~hours after delegation | medium | open |

## Remaining open (26)

Sorted by priority then sequence.

**Urgent (1):**
- MWAN-10 Remove leaked .api-credentials from testbed repo (creds in vault only)
- MWAN-14 Cutover2: production deployment

**High (8):**
- MWAN-1 Revalidate testbed and do deep dive of edge cases / verification / core cases
- MWAN-4 NPT rules persist across nft -f / systemctl restart nftables
- MWAN-7 IPv6-first across every component
- MWAN-12 Verify on suburban testbed
- MWAN-18 Fix NPT rules lost on nftables reload (duplicate of MWAN-4 essentially)

**Medium (8):**
- MWAN-3 Pull /60 PD instead of /64 so NPT matches
- MWAN-5 Handle LXC 203 no-internet case via temporary uplink or offline build
- MWAN-6 WARP profile include path must cover new static route
- MWAN-9 j2-template static config files (config.toml, nftables.conf) under Ansible
- MWAN-13 OPNsense upgrade to 26.x with rollback wired into Go monolith
- MWAN-15 Refactor mwan CLI into proper CLI parser
- MWAN-17 Create standalone masquerade fallback script for interfaces without PD
- MWAN-19 Reduce ~8s IPv4 gap during cutover2 switch-to-bgp
- MWAN-37 Pre-populate ARP/NDP entries for .3 on OPNsense before cutover
- MWAN-38 FRR route-map name collision from previous partial cutover runs
- MWAN-40 OPNsense agent entrypoint in Go monolith
- MWAN-46 Monkeybrains DHCPv6-PD lease silently lapses

**Low (8):**
- MWAN-8 One file per service/host, remove old cruft, clean /etc/interfaces at end
- MWAN-11 Factor NPT rule builder out of update-npt.sh into its own script
- MWAN-16 Move health-check targets to config.toml
- MWAN-20 Delete superseded static files in mwan/testbed/vm-950/
- MWAN-21 Split prep-guests into setup vs ongoing maintenance
- MWAN-22 Convert LXC 100 static configs to .j2 templates
- MWAN-44 networkd-dispatcher: enable INFO logging
