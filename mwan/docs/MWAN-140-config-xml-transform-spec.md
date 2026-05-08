# MWAN-140 slice 4: planning spec for the prod-to-testbed config.xml transform

Tracking ticket: `MWAN-140`. Slice 4 of the parity plan in `mwan/docs/MWAN-140-testbed-infra-parity-plan.md`. This document is a planning artifact only. It does not implement the transform. The implementation lives in a follow-up ticket suggested at the end of this document.

## 1. Why

The testbed has no PCI passthrough, so it has no `iavf0` device. Suburban hosts allocate the OPNsense testbed VM (VM 101) a single virtio NIC on the new VLAN-aware bridge `vmbrtrunk` (codified in `opentofu/vms.tf` per slice 2 of MWAN-140). That single NIC carries MANAGEMENT untagged plus the four 802.1q VLAN children (vlan0064, vlan0100, vlan0200, vlan0300) inside the guest, mirroring prod's one-port posture (per `MWAN-148`). With one NIC declared on VM 101, the guest device name is the FreeBSD virtio default for net0, which is `vtnet0`. When the redacted prod config.xml is imported on the testbed without a transform, every `<if>iavf0</if>` reference resolves to a device that does not exist on the guest, so OPNsense reports the interface as missing and the dependent VLAN children, firewall rules, gateways, and DHCP scopes all fail to bind.

`MWAN-148` rejected the FreeBSD `rc.conf` rename approach (renaming `vtnet0` to `iavf0` via `ifconfig_<orig>_name=`) and centralized the device-name asymmetry in this transform layer. The transform is therefore the single point where prod-shaped device names become testbed-shaped device names. The transform also handles every other prod-specific value that would conflict with testbed (addressing, hostnames, WireGuard peers, NAT64 prefixes if any clash).

Reference for the redacted prod artifact this transform consumes: `/Users/agoodkind/Sites/configs/.claude/worktrees/mwan-redact-opnsense-config/tmp/opnsense-prod-config.redacted.xml`.

Assumption flagged: VM 101 sees its trunk NIC as `vtnet0` because slice 2 declares exactly one `network_device` block. If a follow-up slice adds a second NIC (for example, a separate management NIC or a BGP-side NIC), the guest device names shift and the transform's target name needs to follow.

## 2. Scope: every `iavf0` reference in the redacted prod config.xml

Verified by `grep -n "iavf0" opnsense-prod-config.redacted.xml` at the prod artifact path above on 2026-05-07. The artifact has exactly five matches.

| Line | XPath context | Element |
| --- | --- | --- |
| 414 | `/opnsense/interfaces/opt9/if` | MANAGEMENT slot, `<if>iavf0</if>` |
| 5681 | `/opnsense/vlans/vlan[1]/if` | GeneralVLAN tag 200 parent, `<if>iavf0</if>` |
| 5689 | `/opnsense/vlans/vlan[2]/if` | IPv6OnlyVLAN tag 64 parent, `<if>iavf0</if>` |
| 5697 | `/opnsense/vlans/vlan[3]/if` | PrivilegedVLAN tag 100 parent, `<if>iavf0</if>` |
| 5705 | `/opnsense/vlans/vlan[4]/if` | CaptiveVLAN tag 300 parent, `<if>iavf0</if>` |

Target rewrite: every `iavf0` becomes `vtnet0` (or whichever device the testbed VM 101's single trunk NIC binds to at boot). Verification step before applying the transform: SSH to the rebuilt VM 101 and confirm `ifconfig -l` lists the target name.

Adjacent name references that do NOT mention `iavf0` but bind to the same parent through their VLAN children. These need to stay as `vlan0100`, `vlan0200`, `vlan0300`, and `vlan064` because the OPNsense VLAN engine generates those names from the parent and tag. The tags are the contract, the VLAN names are derived. The transform does not touch them, which keeps the firewall rules, gateways, and DHCP scopes that reference `vlan0100` (etc.) intact.

Sanity check the transform does not miss a sixth match: also grep for `iavf` (no trailing digit) to catch any future `iavf1`, `iavf2`, or `iavfN.M` reference. The current artifact has zero matches for that pattern, so the five entries above cover every prod reference today. The transform should still grep for the broader pattern in CI to catch new arrivals.

## 3. Other testbed-specific rewrites

The redacted prod config.xml carries several prod-specific values that need testbed equivalents. The list below comes from grepping the redacted artifact for the most common prod-only literals; operator review may reveal more.

### 3.1 IP address rewrites

Prod uses 10.250.0.0/16 and 3d06:bad:b01::/56 broadly. Testbed reserves 10.240.0.0/16 and 3d06:bad:b01:200..209::/56 to avoid collisions with prod (per the MWAN-140 parity plan, risk callout 2).

Per-interface mapping the transform applies, sourced from the prod redacted artifact and the testbed convention:

| Prod literal | Testbed literal | Source line in artifact | Notes |
| --- | --- | --- | --- |
| `10.250.250.2` (WAN ipv4) | `10.240.250.2` or operator-chosen | line 316 | WAN side already shifts in testbed because the simulated ISP LXCs run on `vmbr2`/`vmbr4..6` |
| `3d06:bad:b01:fe::2` (WAN ipv6) | `3d06:bad:b01:2fe::2` | line 318 | follows the `:200..209::` testbed prefix family |
| `10.250.1.1` (PRIVILEGED v4) | `10.240.1.1` | line 327 | |
| `3d06:bad:b01:1::1` (PRIVILEGED v6) | `3d06:bad:b01:201::1` | line 329 | |
| `10.250.2.1` (GENERAL v4) | `10.240.2.1` | line 366 | |
| `3d06:bad:b01:2::1` (GENERAL v6) | `3d06:bad:b01:202::1` | line 368 | |
| `10.250.3.1` (CAPTIVE v4) | `10.240.3.1` | line 376 | |
| `10.250.0.1` (VMNET v4) | `10.240.0.1` | line 385 | |
| `3d06:bad:b01::1` (VMNET v6) | `3d06:bad:b01:200::1` | line 387 | conflicts with vault `3d06:bad:b01::254/64` if not rewritten |
| `3d06:bad:b01:64::1` (IPv6OnlyVLAN v6) | `3d06:bad:b01:264::1` | line 410 | |
| `10.250.4.1` (MANAGEMENT v4) | `10.240.4.1` | line 418 | |
| `3d06:bad:b01:4::1` (MANAGEMENT v6) | `3d06:bad:b01:204::1` | line 420 | |

The third column gives the line where each literal first appears in the redacted artifact. The exact testbed values are open question 7.1 below, which the operator decides before the transform runs.

### 3.2 DHCP pools and ranges

| Prod literal | Source line | Notes |
| --- | --- | --- |
| `10.250.1.50` to `10.250.1.150` (PRIVILEGED pool) | 442 to 443 | shifts to whatever subnet PRIVILEGED takes on testbed |
| `10.250.2.10` to `10.250.2.45` (GENERAL pool) | 455 to 456 | same shift |
| `10.250.3.3` to `10.250.3.250` (CAPTIVE pool) | 478 to 479 | same shift |

Each pool follows the same prefix as its interface, so the transform reuses the per-interface map above to derive the new low/high pair.

### 3.3 NAT and outbound rules

Prod's outbound NAT and any rules that reference `10.250.0.0/24` in `<source_networks>` (line 131 of the artifact) need to point at the testbed equivalents. The transform should rewrite the source/destination lists wherever a prod prefix appears.

Open question 7.2 covers whether to preserve the firewall rule UUIDs or regenerate them; preserving them keeps prior referencing (logs, alerts) stable but risks UUID collision if the transform ever lands on a host that already imported a real prod backup.

### 3.4 Hostname and identity

| Prod literal | Source line | Testbed value |
| --- | --- | --- |
| `<hostname>router</hostname>` | 113 | `router-test` or operator-chosen |
| `<domain>home.goodkind.io</domain>` | 114 | `test.home.goodkind.io` (subdomain to avoid DNS collision with the prod router record) |
| `<althostnames>router.home.goodkind.io home.goodkind.io</althostnames>` | 225 | tracks the new hostname/domain |
| `<dnssearchdomain>home.goodkind.io</dnssearchdomain>` | 305 | matches the new domain |

The hostname appears in dozens of places downstream (RA settings, certificate CNs, SSH banners). The transform should rewrite the literal everywhere it appears, not just in the `<system>` block. A whole-document substitution is safer than a list of paths.

### 3.5 WireGuard peers

The prod `<wireguard>` block (lines 2707 onward) carries peer keys, endpoints, and allowed-IP lists. Reusing prod peer keys on testbed means a misrouted handshake from one side could land on the other (MWAN-140 risk callout 3). The transform should:

- Strip every prod peer entry, OR
- Replace every peer's public key, endpoint, and preshared key with testbed-only values.

Open question 7.3 covers which path the operator wants. Stripping is simpler and safer; replacing preserves the rule shape so testbed exercises the same code paths as prod.

### 3.6 NAT64 / Tayga prefix

Prod uses `3d06:bad:b01:6464::/96` (line 5715) for the NAT64 mapping. If the testbed routing plane shares any reachability with prod over the management plane, the testbed Tayga should use a non-overlapping prefix. Likely candidate: `3d06:bad:b01:2664::/96`. Open question 7.4 covers the final value.

### 3.7 Captive portal domain

Prod uses `captive.chaotic.dog` (line 4270 area, plus `<hostname>captive</hostname>` and `<domain>chaotic.dog</domain>` at 4323 to 4324). Testbed should use a clearly distinct value (e.g. `captive.test.home.goodkind.io`) so a misrouted captive portal redirect cannot land a tester on prod.

### 3.8 Certificate material

The `<altNames>*.home.goodkind.io</altNames>` and the certificate CN at line 4412 area embed the prod domain. The transform should rewrite the CN and altNames to match the testbed domain, OR strip the inline cert and let OPNsense regenerate one on first boot. Open question 7.5 covers the choice.

### 3.9 SSH usernames embedded in firewall rules

Many `<username>` entries (lines 519, 540, 548, 572, 577, 603, 608, 635, 640, 656, 666, 671, 698, 703, 730, 735, 762, 767, 794, 799, etc.) bind to specific prod operator IPv6 addresses like `agoodkind@3d06:bad:b01::110`. On testbed those addresses do not resolve, so the rules either need rewriting to testbed admin addresses or stripping to a permissive testbed default. Open question 7.6 covers the choice.

## 4. Approach options

Three implementation paths, each with trade-offs.

### 4.1 Pure XML edit via `sed` or `yq` style substitution

Pros: small, scriptable, easy to read in a code review, no compile step.
Cons: brittle when the schema shifts. A `sed s/iavf0/vtnet0/g` rewrites every literal `iavf0` regardless of context, which is fine for `iavf0` (the literal appears nowhere else) but dangerous for `10.250.1.1` if any non-IP value happens to share that string. Also, OPNsense persists XML elements with attributes and CDATA; sed or a flat string replace can corrupt those if the boundaries are not clean.

### 4.2 XML-aware transform via `xmlstarlet` or Go `encoding/xml`

Pros: structural rewrites, safe against accidental literal collisions, easy to test against XPath expectations.
Cons: more code than sed; OPNsense's config.xml is large (6316 lines in the redacted artifact) so a streaming or DOM walker is needed; `encoding/xml` round-tripping reorders attributes and re-encodes whitespace, which produces a noisy diff against the source artifact.

### 4.3 Go subcommand under `mwan` that reads, transforms, writes (recommended)

Pros: lives next to the rest of the mwan binary, integrates with the existing testing harness (`make check`), reuses the `internal/opnsense` package which already models the RPC channel, can carry a structured substitution table defined in code that is easy to review and unit test, and can ship with a round-trip test using the redacted artifact as the golden input. The substitution table is the single source of truth for "what changes between prod and testbed", which makes the parity matrix (slice 7) self-documenting.
Cons: new subcommand surface area; needs careful XML round-trip handling so the resulting file is still OPNsense-readable (OPNsense persists config.xml with specific element ordering and attribute placement).

Recommended path: 4.3 with hybrid handling. Use `encoding/xml` to walk the tree and apply structural substitutions where the path matters (interfaces, VLANs, peers), and a small list of textual substitutions applied after the round-trip for whole-document literals like the domain and hostname. Tests assert both per-element correctness and overall textual stability against a frozen golden file.

## 5. Testability

Three test layers.

### 5.1 Round-trip golden test

Input: `tmp/opnsense-prod-config.redacted.xml` (the artifact already in the worktree).
Output: a frozen `tmp/opnsense-testbed-config.golden.xml` checked into the repo.
Assertion: running the transform on the input produces the golden output byte-for-byte. This test pins the transform's behavior so future edits surface as diffs in code review.

### 5.2 Per-element XPath test

For each path in the scope table (section 2) and the rewrite table (section 3), assert the post-transform value matches the testbed expectation. This catches regressions where a code change misses a specific element. Use `encoding/xml` plus a small XPath-like query helper (or `github.com/antchfx/xmlquery`).

### 5.3 Structural validity test

After the transform, the output must still be valid OPNsense config.xml. OPNsense itself does not ship a public XSD, but two practical checks exist:

1. Round-trip through `xmllint --noout` to confirm well-formedness.
2. Boot a throwaway OPNsense VM with the transformed config and verify `configd` accepts it. This test runs in the slice 6 rebuild, not in CI; it is the integration gate.

Open question 7.7 covers whether to ship a stronger structural test in CI (e.g. an offline OPNsense schema check via the `php-cs-fixer` style validator, or a partial XSD covering the subset of elements the transform touches).

## 6. Where the implementation lives

Recommended path: `mwan/go/cmd/mwan/opnsense_import.go` plus a sibling test file. The subcommand surface follows the existing pattern (`mwan opnsense-host`, `mwan opnsense-daemon-serve`, etc.). Suggested name: `mwan opnsense-import-config`. Invocation:

```
mwan opnsense-import-config \
  --input  /path/to/opnsense-prod-config.redacted.xml \
  --output /path/to/opnsense-testbed-config.xml \
  --substitutions /path/to/substitutions.yaml
```

Substitution table format: YAML, with sections for `device_names`, `ipv4_remap`, `ipv6_remap`, `hostname`, `domain`, `wireguard_peers`, `nat64_prefix`, etc. The default values mirror the tables in section 3, sourced from `mwan/docs/MWAN-140-config-xml-transform-spec.md` (this file). Operators override per-environment values via the YAML.

The `internal/opnsense` package already models the OPNsense-side RPC. The transform code is a sibling concern (parsing and rewriting config.xml on the host before SSH'ing it to the guest), so it can live in a new sibling package `mwan/go/internal/opnsense/configxform/` to avoid bloating the RPC client.

## 7. Open questions for operator input

Listed as numbered items so the follow-up ticket can answer them in order.

1. **Final testbed network ranges.** Section 3.1 proposes a mechanical shift from `10.250.x.y` to `10.240.x.y` and from `3d06:bad:b01:N::` to `3d06:bad:b01:2N::`. Confirm this matches the existing testbed convention captured in `ansible/inventory/group_vars/mwan_testbed_servers.yml` and the suburban host bridge addressing (e.g. `vmbr1` at `10.240.200.1/24`, `3d06:bad:b01:200::1/64`). The exact per-VLAN testbed prefixes need an explicit list.
2. **Firewall rule UUIDs.** Preserve them (stable cross-environment IDs, simple) or regenerate them (no chance of cross-environment collision)?
3. **WireGuard peers.** Strip them entirely, or replace with testbed-only peers? If replace, what is the testbed peer key set?
4. **NAT64 prefix.** Use `3d06:bad:b01:2664::/96` to avoid collision with prod's `3d06:bad:b01:6464::/96`, or pick a different non-overlapping value?
5. **Inline TLS certificates.** Rewrite the CN and altNames to match the testbed domain, or strip the inline certs and let OPNsense regenerate on first boot?
6. **SSH-username-bound firewall rules.** Rewrite each `agoodkind@<prod-ipv6>` to a testbed-side admin address, or strip them in favor of a permissive testbed default?
7. **CI structural validation.** Ship an offline schema check (xmllint plus a partial XSD), or accept the slice 6 boot gate as the only structural validator?
8. **VLAN tag remapping.** Keep prod tags 64, 100, 200, 300 on testbed (current plan), or remap to disjoint testbed tags so a single trunk that ever spans both planes can carry both? Default: keep tags, since the trunk does not span planes.

## 8. Follow-up ticket suggestion

Title: `MWAN-140 slice 4: implement opnsense-import-config transform (Go subcommand under mwan)`

Scope:

- Add `mwan opnsense-import-config` subcommand at `mwan/go/cmd/mwan/opnsense_import.go`.
- Add the transform package at `mwan/go/internal/opnsense/configxform/`.
- Implement substitution table loading from YAML, with defaults baked in to match the tables in this spec.
- Implement device-name rewrite (section 2), IP rewrites (section 3.1 and 3.2), hostname/domain rewrites (section 3.4), WireGuard handling (section 3.5) per the operator's chosen option in 7.3, NAT64 prefix rewrite (section 3.6), captive portal rewrite (section 3.7), certificate handling (section 3.8) per 7.5, SSH-username handling (section 3.9) per 7.6.
- Ship a round-trip golden test (section 5.1) using the redacted artifact as input.
- Ship per-element XPath tests (section 5.2) covering every entry in the rewrite tables.
- Update `mwan/docs/MWAN-140-testbed-infra-parity-plan.md` to reference the implementation path and remove the placeholder language in the slice 4 section.
- Out of scope: actually applying the transformed config.xml to a live OPNsense VM. That work belongs to slice 6.

Acceptance: golden test passes, per-element tests pass, `xmllint --noout` on the output is clean, `make check` is green.

## 9. Implementation status

Tracking ticket: `MWAN-150` (slice 4 implementation). The Go code lives in:

- Subcommand entry: `mwan/go/cmd/mwan/opnsense_import_config.go` (subcommand name: `mwan opnsense-import-config`).
- Transform package: `mwan/go/internal/opnsense/configxform/` (`configxform.go`, `yaml.go`, `configxform_test.go`).
- Test fixture: `mwan/go/internal/opnsense/configxform/testdata/minimal-config.xml`.
- Operator-facing example YAML: `mwan/testbed/opnsense/substitutions.example.yaml`.

The YAML schema has four sections:

1. `device_names`: `from` and `to` text values for `<if>` element rewrites (spec section 2).
2. `xpath_sets`: per-element `xpath` plus `new_value` (spec sections 3.1, 3.4, 3.6, 3.7).
3. `remove_elements`: `xpath` to delete (spec section 3.5 strip option, 3.8 strip option).
4. `text_literals`: byte-level `from` and `to` for prefix shifts and embedded references (spec sections 3.3, 3.7, 3.9).

The example YAML carries placeholder values for every operator-decided field, marked `TBD: see MWAN-140 open question N` per spec section 7. Real-world use needs the operator to resolve those open questions and replace each TBD value before invoking the transform.

The implementation deliberately treats Apply as a single in-process transform pass. The structural XML walks run first using `beevik/etree`, and the byte-level text replacements run last on the serialized output so any embedded copies (cert CNs, NAT source nets, SSH-username addresses) are caught after the canonical element values are set. Tests in `configxform_test.go` cover the round-trip device-name rewrite, per-element XPath rewrite, text literal rewrite, WireGuard peer strip, and the YAML decoder error paths (malformed YAML, unknown fields, empty input).

## 10. MWAN-150 resolution of section 7 open questions

Tracking ticket: `MWAN-150`. The concrete substitution table that resolves the open questions below lives at `mwan/testbed/opnsense/substitutions.yaml`. The example YAML at `mwan/testbed/opnsense/substitutions.example.yaml` documents the same decisions with the alternatives spelled out so a future operator can revisit any choice.

The resolution was verified against suburban live state on 2026-05-08 (`cat /etc/network/interfaces` and `cat /etc/network/interfaces.d/testbed-masquerade.conf` over v6 SSH, read-only) and against the existing testbed conventions captured in `ansible/inventory/group_vars/mwan_testbed_servers.yml` and `mwan/config/suburban-testbed.toml.j2`.

### 10.1 Resolved with concrete values

1. **Final testbed network ranges (question 1).** The five VLAN subnets shift mechanically: prod `10.250.{0,1,2,3,4}.x/24` becomes testbed `10.240.{0,1,2,3,4}.x/24`, and prod `3d06:bad:b01:{0,1,2,3,4}::/64` becomes testbed `3d06:bad:b01:{200,201,202,203,204}::/64`. The IPv6OnlyVLAN shifts from `3d06:bad:b01:64::/64` to `3d06:bad:b01:264::/64`. The WAN-side IPv4 link `10.250.250.0/29` is reused unchanged because the live testbed already runs that subnet (suburban `vmbr2` at `10.250.250.5/29`, VM 950 internal at `10.250.250.3` per the testbed group_vars). The WAN-side IPv6 subnet shifts from prod `3d06:bad:b01:fe::/64` to testbed `3d06:bad:b01:201::/64` so the OPNsense WAN IPv6 ends up at `3d06:bad:b01:201::2`, matching `mwan_opnsense_edge_ipv6` in the testbed group_vars.
4. **NAT64 prefix (question 4).** Prod uses `3d06:bad:b01:6464::/96`; testbed uses `3d06:bad:b01:2664::/96` to keep the prefix shape parallel while staying in the testbed `:200..2ff::` family.
8. **VLAN tag remapping (question 8).** Keep the prod tags `64`, `100`, `200`, `300` on testbed. The trunk does not span planes, so there is no risk of cross-plane traffic. Suburban `vmbrtrunk` already declares `bridge-vids 64 100 200 300` per the live `/etc/network/interfaces`.

### 10.2 Resolved as design choices with documented defaults

2. **Firewall rule UUIDs (question 2).** Default: preserve. UUID changes do not affect functionality but block log-line correlation against any frozen prod artifact and make per-rule reasoning across the two planes harder. The transform leaves UUIDs alone unless the operator adds explicit XPath rewrites. Alternative: regenerate, only worth picking if there is a real risk that the testbed config ever gets reimported on prod (today there is not).
3. **WireGuard peers (question 3).** Default: strip. The testbed does not need real upstream peers; an isolated tunnel that does not actually connect anywhere is fine for parity exercises and avoids the misrouted-handshake risk callout 3 in the parity plan. Alternative: replace each peer with a testbed-only key set, which exercises more code paths but adds key-management overhead.
5. **Inline TLS certs (question 5).** Default: strip. OPNsense regenerates a self-signed cert on first boot. Alternative: rewrite CN and altNames to match the testbed domain, which preserves cert state but embeds material that has no real chain of trust on testbed anyway.
6. **SSH-username-bound firewall rules (question 6).** Default: rely on the v6 prefix shifts above to rewrite the parallel addresses (`agoodkind@3d06:bad:b01::110` becomes `agoodkind@3d06:bad:b01:200::110` via the VMNET shift) and accept that the remaining prod operator addresses (e.g. `agoodkind@3d06:bad:b01:10::8`, `agoodkind@10.250.1.245`) resolve to non-existent testbed addresses after the shifts. The rules keep their shape so OPNsense exercises the same code paths on import, but no operator matches them. Alternative: strip every `<username>`-bound rule outright if a cleaner rule set matters more than parity with the prod rule shape.

### 10.3 Deferred

7. **CI structural validation (question 7).** Deferred to slice 6 boot gate per spec section 5.3. The transform output passes `xmllint --noout` for well-formedness; the slice 6 from-scratch boot test on VM 102 acts as the structural validator. No XSD shipped in CI.

### 10.4 Notes for the operator

- The text-literal substitution order in `substitutions.yaml` is load-bearing: the longer/more-specific IPv6 prefixes must come before the shorter ones, and `chaotic.dog` must come after `captive.chaotic.dog`. The file's comments call this out per substitution. Reordering by hand needs care.
- The transform deliberately leaves DHCP pool ranges (lines 442-479 of the redacted artifact) to the text-literal layer because the prefix-shift entries above already cover them: `10.250.1.50` to `10.250.1.150` becomes `10.240.1.50` to `10.240.1.150` via the PRIVILEGED v4 shift, and so on. No separate xpath entries are needed for the DHCP pools.
- The MANAGEMENT interface rewrite assumes prod's `<opt9>` slot continues to host MANAGEMENT. If a future redact swaps the slot, both `xpath_sets` entries above (`opt9/ipaddr`, `opt9/ipaddrv6`) need updating. Verify with `grep -n MANAGEMENT` against the next redacted artifact before applying.

### 10.5 opt6 collision finding (2026-05-08)

Pre-reboot inspection of VM 102 against the imported testbed-shaped config caught a critical collision: opt6 (VMNET) declares `<if>vtnet0</if>` directly, and opt9 (MANAGEMENT) declares `<if>iavf0</if>` which the device-name mapping rewrites to `vtnet0`. After import, OPNsense binds `vtnet0` to opt6 first by alphabetical priority and leaves opt9 (the management interface holding `10.240.4.1` and `3d06:bad:b01:204::1`, which is the SSH path on testbed) without an address. The fix is a `remove_elements` entry that strips `//opnsense/interfaces/opt6` entirely; suburban testbed has no VMNET-equivalent broadcast domain so the strip carries no functional loss. The text-literal VMNET prefix shifts still rewrite embedded VMNET copies in firewall rules, NAT rules, and aliases, leaving those references pointed at non-existent testbed addresses (the rules retain their shape but cannot match, which is acceptable for parity).

When future device-name rewrites add new untagged-vtnet0 entries, audit `<interfaces>/*` for `<if>vtnet0</if>` children that lack a `<vlanif>` or `<vlanparent>` sibling. Exactly one such interface should remain (the management slot holding the SSH path); any additional collision must be stripped or rewritten.
