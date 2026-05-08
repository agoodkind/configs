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
| `plugin_os_captiveportal_installed` | `pkg info os-captiveportal` | exit 0. Skip if not in baseline. | regression |
| `plugin_os_captiveportal_zones_active` | `configctl captiveportal list_zones` | non-empty list when baseline had zones. | regression |
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
