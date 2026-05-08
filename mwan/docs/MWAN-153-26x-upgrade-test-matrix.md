# MWAN-153: OPNsense 26.x Upgrade Test Matrix

Status: design (no implementation in this branch).
Scope: validation surface for an OPNsense major-release upgrade (25.7 to 26.x).
Related: MWAN-152 (rollback flow this matrix feeds into), MWAN-140 (testbed
parity), MWAN-130 (BGP graceful restart).

## 1. Why a test matrix

The upgrade succeeds when every functional surface that worked on 25.7 still
works on 26.x. We cannot prove that by clicking around in the GUI, so we need
a fixed list of automated checks that produce a pass or fail signal per
surface, run from a controller (the Proxmox host or a workstation) over SSH
and over the existing `mwan opnsense-host` gRPC bridge.

Two design principles drive the rest of this document.

- Each check is a single command or API call with a deterministic exit code,
  regex match, or JSON field equality. Anything that requires human
  interpretation does not belong here.
- Checks are run twice. Once on 25.7 immediately before the upgrade to
  capture a baseline, then on 26.x after the upgrade. The diff between the
  two runs is what we evaluate, not absolute expected values, since some
  values vary per environment (peer counts, plugin versions, certificate
  serial numbers).

References for the surfaces below:

- `mwan/config/production.toml.j2` (configured features on prod vault).
- `mwan/OPNSENSE-OPERATIONAL-NOTES.md` (steady-state OPNsense config).
- `AGENTS.md` (service inventory).
- `mwan/go/internal/healthcheck/main.go` (existing ping4/ping6/HTTP probe primitives).
- OPNsense source tree on the target host: `/usr/local/etc/inc/`,
  `/usr/local/opnsense/`, `/conf/config.xml`.

## 2. Functional surfaces and their checks

Every check below has a machine-readable name in `snake_case`, a command or
API call, a pass criterion, and a severity. Severities are:

- `blocker` -> upgrade is not considered successful, trigger MWAN-152 rollback.
- `regression` -> upgrade landed but a feature is broken, file a follow-up
  before declaring success.
- `advisory` -> a value changed in an expected-but-worth-noting way, e.g.
  plugin version bump.

### 2.a Routing and forwarding

| Check name | Command or API | Pass criterion | Severity |
|---|---|---|---|
| `bgp_v4_neighbor_established` | `vtysh -c 'show bgp ipv4 unicast summary json'` (parse `peers.<addr>.state`) | every neighbor address has `state == "Established"` for both `10.250.250.3` (mwan agent) and `10.250.250.4` (mwan-failover LXC). Source: `production.toml.j2` lines 106-112 and `OPNSENSE-OPERATIONAL-NOTES.md` lines 71-73. | blocker |
| `bgp_v6_neighbor_established` | `vtysh -c 'show bgp ipv6 unicast summary json'` | same, for `3d06:bad:b01:fe::3` and `3d06:bad:b01:fe::4`. | blocker |
| `bgp_default_v4_installed` | `vtysh -c 'show ip route 0.0.0.0/0 json'` | exactly one `bgp` entry with `installed: true` and `selected: true`. Source: `OPNSENSE-OPERATIONAL-NOTES.md` line 65. | blocker |
| `bgp_default_v6_installed` | `vtysh -c 'show ipv6 route ::/0 json'` | same for v6. | blocker |
| `kernel_default_v4_present` | `netstat -rn -f inet \| awk '$1=="default"{print $2}'` | output non-empty and matches the BGP next-hop captured in baseline. | blocker |
| `kernel_default_v6_present` | `netstat -rn -f inet6 \| awk '$1=="default"{print $2}'` | same for v6. | blocker |
| `nat44_egress_works` | from a LAN client (suburban testbed: LXC 100, prod: any vault VM): `curl -4 -m 5 -o /dev/null -w '%{http_code}' http://ifconfig.co/ip` | exit 0 and HTTP 200. Reuses the primitive in `internal/healthcheck/main.go::httpCheck`. | blocker |
| `nat64_v6_only_to_v4_works` | from a v6-only LAN client: `ping6 -c 1 -s 16 64:ff9b::1.1.1.1` (synthesized AAAA via Tayga; if the network uses the `3d06:bad:b01:6464::/96` prefix per `OPNSENSE-OPERATIONAL-NOTES.md` line 28, substitute that) | exit 0. The `-s 16` payload guards against the Webpass small-ICMPv6 drop documented in memory. | regression (blocker only if NAT64 clients exist in env) |
| `outbound_nat_rules_loaded` | `pfctl -sn \| grep -c 'nat on '` | count >= 2, matching the manual rules in `OPNSENSE-OPERATIONAL-NOTES.md` table at line 44. | blocker |
| `wireguard_handshake_recent` | `wg show all latest-handshakes` | every configured peer has `latest-handshake` within `3 * keepalive` of the current time. List of expected peers comes from baseline. | regression |

### 2.b DNS and DHCP

| Check name | Command or API | Pass criterion | Severity |
|---|---|---|---|
| `dns_resolves_external` | from a LAN client: `dig +short +time=3 +tries=1 @<opnsense_lan_ip> ifconfig.co` | exit 0 and at least one A record. | blocker |
| `dns_resolves_internal` | `dig +short @<opnsense_lan_ip> router.home.goodkind.io` | exit 0 and an A record matching baseline. | regression |
| `unbound_running` | `pgrep -f unbound \| wc -l` (over SSH on OPNsense) | >= 1. | blocker |
| `dhcpv4_leases_present` | `cat /var/dhcpd/var/db/dhcpd.leases \| grep -c '^lease '` | count >= baseline count minus an operator-defined tolerance (open question O-1). | regression |
| `dhcpv6_ia_na_present` | `cat /var/dhcpd/var/db/dhcpd6.leases 2>/dev/null \| grep -c 'ia-na '` | >= baseline. Skip if `dhcpd6` not configured (decided from baseline). | regression |
| `dhcpv6_ia_pd_present` | same source, `grep -c 'ia-pd '` | >= baseline. Skip if not configured. | regression |
| `radvd_announcing` | `pgrep -af radvd \| grep -c radvd.conf` over SSH | >= 1 per LAN with RA enabled (baseline determines count). | regression |

### 2.c Firewall and pf

| Check name | Command or API | Pass criterion | Severity |
|---|---|---|---|
| `pf_enabled` | `pfctl -si \| awk '$1=="Status:"{print $2}'` | `Enabled`. | blocker |
| `pf_rule_count_within_tolerance` | `pfctl -sr \| wc -l` | within +/- 5 of baseline. A larger drift is `advisory` and warrants a manual diff. | advisory |
| `pf_state_table_growing` | sample `pfctl -si \| awk '/state table/ {getline; print $2}'` twice with 5s gap | second sample > first. Confirms traffic is flowing through pf. | regression |
| `pf_nat_rule_count` | `pfctl -sn \| wc -l` | within +/- 2 of baseline. | regression |
| `pf_blocks_default_in_lan` | from a LAN client try a blocked target (baseline determines a known-blocked dst): `nc -w 2 <blocked_dst> <blocked_port> ; echo $?` | non-zero (connection refused or timeout). | regression |

### 2.d Plugins

For every plugin listed below the check is the same shape: confirm the
plugin is installed, confirm its service is running, and capture its
version. Versions are expected to change on a major upgrade, so a version
bump alone is `advisory`. A missing plugin or a stopped service is
`blocker` for `os-frr`, `regression` for the others.

Plugin list comes from `OPNSENSE-OPERATIONAL-NOTES.md` and from
`production.toml.j2` references. Confirm presence on the live host before
running, since the actual installed plugin set varies and we should not
assume from the repo.

| Check name | Command | Pass criterion | Severity |
|---|---|---|---|
| `plugin_os_frr_installed` | `pkg info os-frr` | exit 0. | blocker |
| `plugin_os_frr_running` | `service frr status \| grep -q 'is running'` | exit 0. | blocker |
| `plugin_os_frr_version` | `pkg info os-frr \| awk '/^Version/{print $3}'` | record value. Value differs from baseline -> `advisory`. | advisory |
| `plugin_os_wireguard_installed` | `pkg info os-wireguard` | exit 0. Skip if not in baseline. | regression |
| `plugin_os_wireguard_running` | `service wireguard status` | exit 0. | regression |
| `plugin_os_tayga_installed` | `pkg info os-tayga` | exit 0. Skip if not in baseline. | regression |
| `plugin_os_tayga_running` | `service tayga status` | exit 0. | regression |
| `plugin_os_tayga_forwarding` | `nat64_v6_only_to_v4_works` from 2.a covers the data-plane proof. | (linked check) | (linked) |
| `plugin_os_captiveportal_installed` | (deprecated) | (replaced by `core_captiveportal_zones_active` in 2.g, since prod runs the core captive portal feature, not the `os-captiveportal` plugin) | (linked) |
| `plugin_os_captiveportal_zones_active` | (deprecated) | (replaced by `core_captiveportal_zones_active` in 2.g) | (linked) |
| `plugin_os_acme_client_installed` | `pkg info os-acme-client` | exit 0. Skip if not in baseline. | regression |
| `plugin_os_acme_client_certs_unexpired` | parse `/var/etc/acme-client/certs/*.crt` with `openssl x509 -enddate -noout` | every cert listed in baseline still parses and `notAfter` is in the future. | regression |
| `pkg_audit_no_vulnerable_packages` | `pkg audit -F` then `pkg audit` | exit 0 (no vulnerable packages). | advisory |

### 2.e mwan integration

| Check name | Command | Pass criterion | Severity |
|---|---|---|---|
| `mwan_opnsense_daemon_running` | on OPNsense: `pgrep -f mwan-opnsense ; echo $?` | exit 0. Source: `mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense`. | blocker |
| `mwan_opnsense_grpc_responding` | from vault: `mwan opnsense-probe --socket /run/mwan-opnsense.sock` | exit 0. Source: `mwan/go/cmd/mwan/opnsense_probe.go`. | blocker |
| `mwan_agent_bgp_session_up` | covered by `bgp_v4_neighbor_established` and `bgp_v6_neighbor_established`. | (linked) | (linked) |
| `qga_channel_responsive` | from vault: `qm guest exec <vmid> -- /bin/true` | exit 0 within 5s. The QGA channel is required for the upgrade flow, so this confirms it survived. | blocker |
| `mwan_opnsense_host_socket_present` | on Proxmox host: `test -S /run/mwan-opnsense-host.sock` | exit 0. | blocker |

### 2.f Web UI and API

| Check name | Command | Pass criterion | Severity |
|---|---|---|---|
| `gui_https_responds` | `curl -k -m 5 -o /dev/null -w '%{http_code}' https://<opnsense_addr>/` | 200 or 302 (redirect to login). | blocker |
| `api_firmware_status_ok` | `curl -k -u <key>:<secret> https://<opnsense_addr>/api/core/firmware/status` | HTTP 200, JSON parses, `status` field present. Source: OPNsense API, `/usr/local/opnsense/mvc/app/controllers/OPNsense/Firmware/Api/FirmwareController.php`. | blocker |
| `api_firmware_running_version_matches_target` | same response, parse `product_version` | starts with `26.` after the upgrade. | blocker |
| `api_auth_rejects_bad_creds` | `curl -k -u baduser:badpass https://<opnsense_addr>/api/core/firmware/status` | HTTP 401. Confirms auth still enforced. | regression |

## 3. Per-surface check definition (canonical schema)

To make the matrix machine-readable for the runner in section 6, every
check is defined as a record with these fields. The runner consumes a list
of these records and produces a result list of the same length.

```text
check_id           : snake_case identifier, unique
category           : one of routing, dns_dhcp, pf, plugins, mwan, web_api
target             : opnsense | vault | proxmox_host | lan_client
command_kind       : ssh_exec | api_call | gprc_call | local_exec
command            : the literal shell command or API path with placeholders
parser             : exit_code | regex:<pattern> | jsonpath:<path>
expected           : literal_value | baseline_value | range:<min>..<max> | not_empty | nonzero | within_tolerance:<n>
severity           : blocker | regression | advisory
depends_on         : list of check_ids that must pass before this check runs
description        : one-line human description
```

Two examples filled in:

```text
check_id     = bgp_v6_neighbor_established
category     = routing
target       = opnsense
command_kind = ssh_exec
command      = vtysh -c 'show bgp ipv6 unicast summary json'
parser       = jsonpath:$.peers.*.state
expected     = literal_value:Established  (asserted for every peer in baseline)
severity     = blocker
depends_on   = [plugin_os_frr_running]
```

```text
check_id     = pf_rule_count_within_tolerance
category     = pf
target       = opnsense
command_kind = ssh_exec
command      = pfctl -sr | wc -l
parser       = exit_code+stdout
expected     = within_tolerance:5  (vs. baseline)
severity     = advisory
depends_on   = [pf_enabled]
```

## 4. Pre-upgrade baseline capture

The baseline run executes the full check list against the live 25.7 host
and writes the results to a JSON artefact. This becomes the source of
truth for any check whose expected value is `baseline_value` or
`within_tolerance`.

Baseline output format:

```text
baseline_artefact:
  schema_version: 1
  captured_at: <iso8601>
  opnsense_version: <semver>          # captured from api_firmware_running_version
  proxmox_node: <hostname>
  vmid: <int>
  results:
    - check_id: ...
      raw_stdout: ...
      raw_exit_code: ...
      parsed_value: ...
```

Rules for baseline:

- Capture happens within a short window before the upgrade (operator-set,
  default 30 minutes), so transient values like DHCP lease counts are
  representative.
- A baseline run that has any `blocker` failing means the host is already
  unhealthy; the upgrade should not proceed. The matrix runner exits
  non-zero in that case.
- The baseline file is stored next to the upgrade snapshot referenced in
  MWAN-152 so the rollback flow can read it.

## 5. Diff strategy

Post-upgrade run produces the same shape as the baseline. The runner walks
the two lists and produces a diff per check:

| Diff outcome | Meaning |
|---|---|
| pass-pass, parsed_value equal | no change, pass. |
| pass-pass, parsed_value differs but `expected` is `baseline_value` and severity is `advisory` | log as advisory. Plugin version bumps land here. |
| pass-pass, parsed_value differs and `expected` is `baseline_value` and severity is `regression` or `blocker` | report as that severity. |
| pass-pass, `expected` is `within_tolerance:N` and delta is within N | pass. |
| pass-pass, `expected` is `within_tolerance:N` and delta exceeds N | report at the check's severity. |
| pass-fail | report at the check's severity. |
| fail-pass | the check was failing before the upgrade. Log as `pre_existing_failure` and do not count against the upgrade. |
| fail-fail | same as above, log as `pre_existing_failure`. |

Aggregate verdict:

- any `blocker` failing post-upgrade -> overall `blocker`, runner returns
  non-zero, MWAN-152 rollback runs.
- no `blocker` failing, any `regression` failing -> overall `regression`,
  runner returns 0 but operator must triage before declaring success.
- only `advisory` differences -> overall `pass`.

## 6. Implementation hints for the runner

Most likely shape: a new subcommand on the mwan monolith,
`mwan opnsense-validate <vmid>`, run from the Proxmox host (vault). Reasons
to put it there rather than in a separate binary:

- AGENTS.md states "There are NO separate Go binaries. New tools become
  subcommands of this monolith."
- The runner needs the same `mwan opnsense-host` gRPC bridge that already
  runs there for the daemon mutation flow.
- It needs SSH to OPNsense for the `vtysh` and `pfctl` checks. The vault
  host already has the SSH key.

Rough flow:

1. `mwan opnsense-validate <vmid> --capture-baseline -o baseline.json`
2. operator runs the upgrade via the existing flow (out of scope here).
3. `mwan opnsense-validate <vmid> --compare baseline.json -o post.json`.
4. exit code drives whether MWAN-152's rollback path triggers.

Reusable primitives:

- `mwan/go/internal/healthcheck/` already has `ping4`, `ping6`, and
  `httpCheck`. The runner should call those rather than reimplement.
- `mwan/go/internal/opnsense/` has the gRPC client used by
  `opnsense-probe`. The runner can extend it to wrap a small set of
  read-only RPCs (firmware status, FRR show commands).
- The SSH transport is the same one used by the cutover flow. We should
  not invent a new auth path.

Test data:

- The matrix should have a unit test that verifies every record has all
  required fields, parses, and has at least one check per category.
- The runner's diff logic gets a table-driven test covering each diff
  outcome row in section 5.

## 7. Open questions

- O-1: DHCP lease tolerance. We need an operator-defined acceptable drop
  in active leases between baseline and post-upgrade, since clients renew
  asynchronously. Default proposal: `tolerance = max(2, 0.1 * baseline_count)`.
- O-2: Captive portal coverage. Does prod actually use os-captiveportal?
  If not, the related checks should be skipped at baseline time rather
  than reported as regressions.
- O-3: Performance checks. Should the matrix include throughput or
  latency probes (iperf3 between vault and a LAN client, ping RTT
  histogram)? Recommendation: out of scope for MWAN-153, file a
  follow-up if we want it.
- O-4: External monitoring integration. If we already publish health to
  an external monitoring system, the matrix should probably check that
  it sees the host as healthy too. Need to confirm what exists today.
- O-5: 26.x-only checks. Does 26.x introduce any new mandatory surface
  (e.g. a new auth model, a new pf syntax)? We will not know until the
  release notes are out. The matrix should be versioned so that
  26.x-only checks can be added without breaking 25.7 baselines.
- O-6: Where to store baseline artefacts long term. Proposal: alongside
  the MWAN-152 snapshot under `/var/lib/mwan/upgrades/<timestamp>/`.

## 8. Follow-up implementation ticket

Title: `MWAN-XXX: implement mwan opnsense-validate runner for upgrade test matrix`

Scope:

- New subcommand `mwan opnsense-validate` with `--capture-baseline` and
  `--compare` modes.
- Embed the matrix from this design doc as a structured Go literal.
- Reuse `internal/healthcheck` primitives and the existing gRPC client.
- Wire the post-upgrade exit code into MWAN-152's rollback decision.
- Unit tests for parser, diff logic, and matrix completeness.
- Manual test on suburban testbed before any prod use.

Out of scope: the upgrade procedure itself, performance benchmarks
(see O-3), and any GUI changes.

---

## 9. Resolved decisions

This section records decisions on the open questions in section 7. It was
appended on May 8 2026 by an investigation pass on the
`mwan-153-questions` branch off `main` at `e7045e0`. Each subsection cites
the artefact or doc that backs the decision so the reasoning is auditable.

### 9.1 O-1 DHCP lease tolerance (resolved: ratify with a settle window)

Decision: ratify the proposed default `tolerance = max(2, 0.1 * baseline_count)`,
and add a 5-minute settle window between upgrade-finalize and the
`dhcpv4_leases_present` / `dhcpv6_ia_na_present` / `dhcpv6_ia_pd_present`
post-upgrade run.

Rationale: the tolerance has to absorb two effects. The first is clients
that disappear off the LAN during the upgrade window (laptops sleeping,
phones moving to LTE) and have not re-DHCPed yet. The second is leases
that expired during the upgrade reboot. The 10% floor with a 2-lease
absolute minimum covers normal LAN churn for both small and large LANs.
The settle window protects against the case where the post-upgrade run
fires while the LAN is still re-establishing; without it a busy LAN can
look like a regression for sixty seconds and then self-heal.

Implementation note: add a `--settle-after-upgrade=5m` flag to the runner
described in section 6, with the default applied to DHCP-related checks
only. Other checks (BGP, kernel routes, plugins) run immediately so the
operator sees blocker conditions as fast as possible.

Source for the tolerance constant: `Section 2.b dhcpv4_leases_present`
already cited the operator-defined tolerance as O-1. No new dependency.

### 9.2 O-2 captive portal in prod (resolved: prod runs the core captive portal feature, not the os-captiveportal plugin)

Decision: prod actively runs OPNsense's core captive portal feature with
one enabled zone. The `os-captiveportal` plugin is not relevant; captive
portal is a core feature in 25.7 and 26.1 (per
`MWAN-151-26x-changelog-deep-dive.md` section 2 row 5). The matrix should
keep captive portal checks in scope, retarget them at the core feature,
and skip them only on environments where the captive portal feature is
disabled.

Evidence (read May 8 2026): the redacted prod config at
`/Users/agoodkind/Sites/configs/.claude/worktrees/mwan-redact-opnsense-config/tmp/opnsense-prod-config.redacted.xml`
declares one zone at line 2882:

```
<captiveportal version="1.0.4" persisted_at="1762661697.73">
  <zones>
    <zone uuid="c561c16d-1165-4df3-8f9f-23626c79fa12">
      <enabled>1</enabled>
      <zoneid>0</zoneid>
      <interfaces>opt5</interfaces>
      ...
      <description>Chaotic Dog Captive Portal</description>
    </zone>
  </zones>
```

Twelve `__captiveportal_zone_0` references appear in pf table aliases
across the file (rules and counts listed at lines 818, 822, 852, 856,
886, 890, 920, 924, 2093, 2097, 2171, 2195), confirming the zone is
wired into the active ruleset.

The installed plugin list (line 255) is
`os-acme-client,os-crowdsec,os-frr,os-git-backup,os-nginx,os-qemu-guest-agent,os-redis,os-tayga`.
`os-captiveportal` is not in that list. Captive portal is a core feature.

The two existing checks `plugin_os_captiveportal_installed` and
`plugin_os_captiveportal_zones_active` in section 2.d are therefore
mis-targeted. Section 9.4 below adds replacement core-targeted checks to
section 2.g and marks the old plugin-targeted rows as deprecated (they are
not removed so the audit trail is preserved).

Per-environment applicability: the matrix runner should mark each captive
portal check with `applies_when = "captiveportal_zones_present_in_baseline"`
so an environment with no zones (e.g. the suburban testbed) skips them
without reporting a regression. The applies-when predicate evaluates the
baseline result, not a static config flag.

### 9.3 O-2 plugin-set update (informational)

While answering O-2 the prod plugin set was found to differ from what
MWAN-151 derived from the testbed config.xml. The prod set adds
`os-crowdsec`, `os-git-backup`, `os-nginx`, and `os-redis` beyond
`os-frr`, `os-qemu-guest-agent`, `os-tayga`, and `os-acme-client`.

Recommendation: extend the section 2.d table with installed/running
checks for `os-crowdsec`, `os-git-backup`, `os-nginx`, and `os-redis` at
`regression` severity, gated on the plugin being present in baseline.
The shape is the same as the existing rows, so the runner does not need
schema changes. None of these plugins is in the upgrade-blocker path
(routing, DNS, NAT, BGP, captive portal); they are auxiliary services.

This recommendation is informational and does not require action in this
ticket. The matrix runner reads the plugin list from baseline, so a
runner that walks `pkg info -a | grep ^os-` and emits a check per row
satisfies this without a hand-maintained list. The follow-up ticket in
section 8 should specify "discover plugins from baseline" as the runner
contract rather than hard-code the list.

### 9.4 O-2 replacement checks (added to section 2.g new surface)

Two new core-targeted checks land in a new section 2.g, "Captive portal
core feature". They target the core feature path that prod actually uses
and replace the deprecated plugin rows in section 2.d:

| Check name | Command | Pass criterion | Severity |
|---|---|---|---|
| `core_captiveportal_zones_active` | over SSH on OPNsense: `configctl captiveportal list_zones` | every zone in baseline appears with the same `zoneid`. Skip if baseline has no zones. Source: prod config zone `c561c16d-1165-4df3-8f9f-23626c79fa12` at line 2882 of `opnsense-prod-config.redacted.xml`. | regression |
| `core_captiveportal_pf_aliases_present` | over SSH on OPNsense: `pfctl -t __captiveportal_zone_0 -T show \| wc -l` for each zone in baseline | exit 0 (the alias exists). Source: 12 references to `__captiveportal_zone_0` in active pf rules in `opnsense-prod-config.redacted.xml`. Skip if baseline had no zones. | regression |

The ApiMutableModelControllerBase POST-only hardening change in 26.1 (per
MWAN-151 section 6) does not affect either check, since both are
`configctl`/`pfctl` reads over SSH, not API calls.

### 9.5 O-3 throughput and latency probes (resolved: out of scope, follow-up filed in section 11)

Decision: throughput and latency probes are out of scope for this matrix.
A follow-up ticket should track continuous performance measurement as a
separate concern.

Rationale: an upgrade-validation matrix proves "the same surfaces still
work" by comparing two point-in-time runs. Performance characterization
needs a stable test bed, a load generator, a multi-minute observation
window, and a separate baseline regimen. Mixing the two conflates "did
the upgrade break something" with "is the box big enough", and the noise
floor on the second question is much higher.

The MWAN-151 risk register identifies one performance-relevant 26.1
change (R5: vtnet LRO off-by-default and `VIRTIO_NET_HDR_F_DATA_VALID`
removed). The 26.x-specific check `vtnet_hwlro_disabled` in section 9.7
covers the configuration-side observation. The data-plane impact is left
to the separate performance ticket.

The follow-up ticket is filed in section 11 below.

### 9.6 O-4 external monitoring integration (resolved: in-monolith watchdog is the external monitor)

Decision: there is no external monitoring stack today. The mwan watchdog
running on the Proxmox host plays the role of an external monitor for
OPNsense, since its probe path goes Proxmox -> OPNsense -> MWAN -> WAN.
The matrix should include a check that the watchdog reports the OPNsense
path healthy after the upgrade.

Evidence (read May 8 2026):

- A grep across `ansible/`, `proxmox/`, and `mwan/` for prometheus,
  grafana, alertmanager, smokeping, librenms, zabbix, netdata, telegraf,
  influxdb, nagios, uptimerobot, healthchecks.io, cronitor, pingdom,
  statuscake returned no actual configuration. The single match was an
  unrelated reference inside MWAN-72.
- The prod plugin set in `opnsense-prod-config.redacted.xml` line 255
  does not include `os-monit`, `os-nrpe`, `os-net-snmp`, `os-statuspage`,
  `os-zabbix-agent`, `os-zabbix-proxy`, `os-prometheus-node-exporter`,
  `os-telegraf`, or `os-haproxy`. None of the standard "agent on the
  firewall, scraper off-host" patterns is in place.
- `<Syslog>` is enabled (line 4198, `<enabled>1</enabled>`,
  `maxpreserve=31`) but `<destinations />` is empty, so no remote syslog
  receiver is configured. There is no off-box log telemetry today.
- The mwan watchdog at `mwan/proxmox/scripts/mwan-watchdog.sh` runs on
  the Proxmox host and probes the path "Proxmox -> OPNsense (default gw)
  -> MWAN internal link -> WAN" (line 13 comment). It writes pre-deploy
  and known-good snapshots and emits state-change emails through
  `internal/notify`. This is the closest thing to external monitoring
  we run.
- `internal/notify` (per `AGENTS.md` lines 114-127) is the chokepoint for
  ifmgr alerts, watchdog alerts, and other transition emails.

Two new checks land in section 2.h, "Off-box health observation":

| Check name | Command | Pass criterion | Severity |
|---|---|---|---|
| `watchdog_path_healthy` | on Proxmox host: tail `journalctl -u mwan-watchdog --since '5 minutes ago' \| grep -E 'state=(OK\|degraded\|fault)' \| tail -1` | last logged state is `OK`. Source: `mwan/proxmox/scripts/mwan-watchdog.sh` lines 13, 170, 189, 347. | blocker |
| `notify_email_path_intact` | on Proxmox host: `mwan notify --self-test --dry-run` (proposed CLI; runner falls back to `mwan agent ping` if the self-test does not exist on the deployed binary) | exit 0. Confirms the alert path that would carry an upgrade failure notice still works. Source: `internal/notify` chokepoint per `AGENTS.md` lines 114-127. | regression |

The matrix does not need a real external SMTP probe; the existing
`internal/notify` self-test (or its fallback) is sufficient to confirm
the path is intact. If a future ticket adds a Prometheus or similar
stack, replace `watchdog_path_healthy` with the appropriate scrape
target check; the row's intent stays the same.

### 9.7 O-5 26.x-specific checks (resolved: six new checks added, sourced from MWAN-151 risks)

Decision: add six 26.x-specific checks to existing surface tables. Each
check ties back to a MWAN-151 risk register entry that has an observable
post-upgrade signal. Risks without an observable signal (R9 plugin
deprecation scan, R12 doc-only line-number drift) remain follow-ups.

The new checks:

| Check name | Surface | Command | Pass criterion | Severity | Risk |
|---|---|---|---|---|---|
| `running_version_is_26x` | 2.f Web UI/API (extends `api_firmware_running_version_matches_target`) | `curl -k -u <key>:<secret> https://<addr>/api/core/firmware/status` parsed for `product_version` | starts with `26.`. Source: MWAN-151 section 1 (canonical upgrade target is 26.1). | blocker | (general) |
| `kernel_default_v4_persists_post_finalize` | 2.a Routing (extends `kernel_default_v4_present`) | `netstat -rn -f inet \| awk '$1=="default"{print $2}'` sampled three times at 30s intervals starting 60s after `firmware-finalize` exits | every sample non-empty and equal. Catches the scenario where `system_routing_configure` flushes the BGP-installed default and FRR has not re-installed it yet. Source: MWAN-151 R1; `OPNSENSE-OPERATIONAL-NOTES.md` recovery snippet "BGP default got wiped on v4 + v6". | blocker | R1 |
| `kernel_default_v6_persists_post_finalize` | 2.a Routing (extends `kernel_default_v6_present`) | `netstat -rn -f inet6 \| awk '$1=="default"{print $2}'` sampled three times at 30s intervals starting 60s after finalize | same as v4. Source: MWAN-151 R1. | blocker | R1 |
| `vtnet_hwlro_disabled` | 2.i (new section, "Kernel and driver defaults") | over SSH: `sysctl -n dev.vtnet.0.tx_hwlro` (and per additional vtnet index in baseline) | value `0` post-upgrade; was likely `1` on 25.7. The flip is the documented 26.1 default change. Source: MWAN-151 section 4 commit `c7cd4884 vtnet: disable hardware TCP LRO by default`. | advisory (not regression: the new default is correct for a forwarding box) | R5 |
| `interfaces_set_unchanged` | 2.i (new section) | over SSH: `ifconfig -l \| tr ' ' '\n' \| sort` | output equals baseline (set equality). Catches the case where the interfaces.inc refactor in 26.1 reorders or drops a sub-interface. Source: MWAN-151 R6, the +553/-507 diff in `src/etc/inc/interfaces.inc`. | regression | R6 |
| `quagga_api_post_only` | 2.f Web UI/API | `curl -k -u <key>:<secret> -X GET https://<addr>/api/quagga/bgp/set` (note GET, not POST) | HTTP 405 or HTTP 400 (method not allowed). Confirms the 26.1 MVC POST-only hardening took effect on the os-frr controllers. Source: MWAN-151 section 6 "mvc: fix CSRF vulnerability in multiple API endpoints by enforcing POST-only requests". | regression | (security posture) |

A new section 2.i ("Kernel and driver defaults") is added by these
rows. The runner does not need schema changes; the existing
`canonical schema` in section 3 covers the new check shapes.

Out of the MWAN-151 risk register, two risks are explicitly not
checkable here:

- R9 (plugin source still calling deprecated `mwexec_bg`/`mwexec`): a
  source-tree scan is the right shape, not a post-upgrade host probe.
  Filed as follow-up in section 11.
- R12 (doc line-number drift in `interfaces.inc`): documentation
  maintenance only. Out of scope.

R3 (CaptivePortal `<roaming>` migration), R4 (Radvd migration), R8
(os-tayga 1.5 behavior) and R11 (Suricata 8) are covered by existing
rows in the matrix (`core_captiveportal_zones_active`,
`api_firmware_status_ok` config-diff, `nat64_v6_only_to_v4_works`, and
the IDS subtree being absent in our config respectively). No new check
needed for those.

### 9.8 O-6 baseline artefact storage location (resolved: align with MWAN-152's state directory)

Decision: store baseline and post-upgrade artefacts under
`/var/lib/mwan/upgrade/<vmid>/<deploy-id>/` on the Proxmox host (vault),
mirroring the layout MWAN-152 already uses. Use these filenames:

- `pre-baseline.json` (the pre-upgrade matrix run, one record per check).
- `post-result.json` (the post-upgrade matrix run, same shape).
- `diff-report.json` (the per-check diff outcomes from section 5).
- `snapshot-meta.json` (Proxmox snapshot name and timestamp, mirrors
  MWAN-152 section 4.6 metadata.json).

Retention: keep the artefact directory alongside the MWAN-152 snapshot
until `mwan opnsense-upgrade commit` runs. After commit, retain
artefacts for 30 days for post-mortem use, then garbage-collect. The
30-day window is longer than MWAN-152's snapshot retention because the
artefacts are small JSON files and historical baselines are useful for
diagnosing slow-burning regressions; the snapshot itself is GBs and
cannot be kept that long. The retention rule is independent and does
not change the MWAN-152 snapshot lifecycle.

Evidence: the path matches MWAN-152 design doc section 4.7 ("`<state_dir>`
defaults to `/var/lib/mwan/upgrade/`") and section 4.4 ("Capture
pre-upgrade state under `<state_dir>/<vmid>/<deploy-id>/`"). The earlier
proposal in the open-question text used `/var/lib/mwan/upgrades/`
(plural with `s`). The MWAN-152 spelling is `upgrade/` (singular). The
singular form wins because changing MWAN-152 would be invasive and the
two designs need to share a directory.

Cross-link: the `mwan opnsense-validate` runner from section 8 should
take a `--state-dir` flag with the same default as MWAN-152's
`mwan opnsense-upgrade`, so the operator only ever sets one path.

### 9.9 Summary of resolutions

- O-1: ratified default with a 5-minute settle window. Resolved.
- O-2: prod runs the core captive portal feature; checks retargeted to
  section 2.g. Resolved.
- O-3: out of scope; follow-up filed in section 11. Resolved.
- O-4: in-monolith watchdog is the external monitor; two new checks in
  section 2.h. Resolved.
- O-5: six 26.x-specific checks added across sections 2.a, 2.f, 2.i,
  sourced from MWAN-151 risks. Resolved.
- O-6: storage path aligned with MWAN-152 (`/var/lib/mwan/upgrade/`,
  singular). 30-day artefact retention chosen. Resolved.

All six open questions in section 7 have a concrete decision.

---

## 10. New surfaces introduced by section 9

These are the new section labels referenced in section 9. They are
listed here so the runner schema in section 3 stays self-contained.

### 2.g Captive portal core feature

Two checks defined in section 9.4. Apply only when baseline records at
least one captive portal zone.

### 2.h Off-box health observation

Two checks defined in section 9.6. The first (`watchdog_path_healthy`)
runs on the Proxmox host; the second (`notify_email_path_intact`) runs
on the Proxmox host as well. Neither runs on the OPNsense guest.

### 2.i Kernel and driver defaults

Two checks defined in section 9.7 (`vtnet_hwlro_disabled`,
`interfaces_set_unchanged`). Both run over SSH on the OPNsense guest.

---

## 11. Follow-up tickets out of section 9

Two follow-ups are recommended off this resolution pass. Neither
blocks the matrix runner ticket in section 8.

1. `MWAN-XXX: continuous throughput and latency monitoring for OPNsense`
   (per O-3). Scope: pick a load generator, define a steady-state and a
   stressed measurement, capture pre- and post-upgrade samples,
   integrate with the watchdog notification path. Out of scope: making
   it a blocker for upgrades.
2. `MWAN-XXX: scan os-frr 1.51 and os-tayga 1.5 source for mwexec_bg/mwexec calls`
   (per MWAN-151 R9). Scope: source-tree grep for the deprecated
   FreeBSD/OPNsense `mwexec_bg(` and `mwexec(` wrappers in the two
   plugin trees we depend on, file an upstream fix or pin to a known
   working version if any are found. Independent of upgrade timing.
