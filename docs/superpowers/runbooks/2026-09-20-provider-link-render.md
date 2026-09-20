# The daemon renders the provider networkd files: testbed cutover

This page records the proof MWAN-491 requires: the testbed cutover to a release
where the daemon writes each rendered provider's systemd-networkd files, the
comparison of those files against the ones the deploy wrote before, and the
boot ordering with the renderer in place. Each result states the command, the
host it ran on, and what was observed. A step with no recorded output did not
happen.

The daemon writes one `.link` and one `.network` per rendered provider from its
entry in `/etc/mwan/network.json`, names each file after the interface with one
fixed numeric prefix, and opens each file with a marker line. AT&T keeps four
hand-authored files. Astound keeps two on the testbed until a separate change
adds its provider entry.

The testbed gateway is the guest named `mwan.suburban.goodkind.io` on the
hypervisor `suburban`. Commands marked "gateway" ran there over ssh through
`suburban`; commands marked "controller" ran on the controller. Times are PDT.

## Outcome

| Proof step | Result | Release |
| --- | --- | --- |
| Testbed cutover with reboot | Passed 2026-09-20 09:56 | 202609201600-e-16dd8f1 |
| Rendered files against the files they replaced | Passed 2026-09-20 11:35, eleven of twelve pairs identical | e |
| Boot ordering with the renderer | Passed 2026-09-20 11:20 | e |
| Deploy prune against the daemon's files | Failed check mode 2026-09-20 10:05, fixed in #457 | main 429a6f62, fix 10846744 |
| Failure cases rerun | Not run | |
| Production check mode and cutover | Not run | |

## Testbed cutover

The testbed pins release `202609201600-e-16dd8f1`, which contains the renderer
at agoodkind/mwan 6c3fc6e2. Configs main carried the matching inventory: #455
(ad29e3c0) removed the webpass `v4_source` line and the six webpass and
monkeybrains templates, and #456 (2e96a7d8) moved the pin.

Gateway, after the deploy and its reboot:

```bash
mwan version
ls -l /etc/systemd/network
cat /var/run/mwan-health.state
```

Observed: `commit=16dd8f1`; twelve files, four of them written by the daemon at
09:54 under the names `20-enwebpass0.link`, `20-enwebpass0.network`,
`20-enmbrains0.link` and `20-enmbrains0.network`; three providers healthy.

The state comparison against the capture taken at release `085fc2b`, before the
cutover, while the deploy still wrote those files:

```bash
ip rule show; ip -6 rule show
ip -br link; ip -br addr
nft list ruleset
```

Observed: policy rules 0 changed lines in both families, links and addresses 0,
firewall ruleset 0.

## The rendered files against the files they replaced

This comparison is the one place the inventory, the template, the loader and
the renderer are all real at once. Both captures are the full text of every
file in `/etc/systemd/network`, the first at release `085fc2b` and the second
at release `e`:

```bash
for f in /etc/systemd/network/*; do echo "== $f"; cat "$f"; done
```

The two sides pair by their `[Match]` block and extension rather than by file
name, because the daemon uses the interface name where the templates used the
provider name. Comment lines and blank lines are stripped from both sides, and
the keys within a section are compared as a set.

Eleven of the twelve pairs are identical:

| Before | After |
| --- | --- |
| `20-webpass.link` | `20-enwebpass0.link` |
| `20-webpass.network` | `20-enwebpass0.network` |
| `30-monkeybrains.link` | `20-enmbrains0.link` |
| `20-att.link`, `20-att.network` | unchanged, the daemon writes neither |
| `30-astound.link`, `30-astound.network` | unchanged, still hand-authored |
| `10-mgmt.link`, `10-mgmt.network` | unchanged, the deploy writes both |
| `40-mwanbr.link`, `40-mwanbr.network` | unchanged, the deploy writes both |

The twelfth pair differs by one key. `30-monkeybrains.network` set `[DHCPv6]
DUIDRawData=` to an empty value, and `20-enmbrains0.network` omits the key: the
testbed sets `mwan_monkeybrains_duid_raw_data` to an empty string, and the
model's DUID pattern rejects an empty value. systemd-networkd applies the same
default for an empty assignment and for an absent key.

## Boot ordering with the renderer

Read the ordering from systemd's own bookkeeping on this guest. The journal's
raw `__MONOTONIC_TIMESTAMP` field disagrees with those timestamps by a
non-constant amount here: `systemd-udev-trigger` reads 16.794 in the raw field
against 9.703 from `systemctl show`, and `systemd-networkd` reads 16.867
against 12.452. `journalctl -o short-monotonic` agrees with `systemctl show`.

Gateway, boot `0da6f387`:

```bash
systemctl show mwan-ifmgr@wan.service systemd-udev-trigger.service systemd-networkd.service -p Id -p ExecMainStartTimestampMonotonic -p ActiveEnterTimestampMonotonic
```

Observed, monotonic microseconds:

| Unit | ExecMainStart | ActiveEnter |
| --- | --- | --- |
| `mwan-ifmgr@wan.service` | 9515267 | 9521218 |
| `systemd-udev-trigger.service` | 9699463 | 10196892 |
| `systemd-networkd.service` | 12450690 | 14418571 |

`systemd-analyze critical-chain mwan-ifmgr@wan.service` reports the daemon at
`@3.470s` after `systemd-remount-fs.service`. The gateway reports `running`
with 0 failed units.

The daemon rewrote nothing at that boot. Its log line reads:

```
msg="ifmgr: networkd unit files written" dir=/etc/systemd/network changed=null
```

The four rendered files already matched what the renderer produces. The daemon
therefore issued no networkd reload. Those files carry mtime 09:54, written by
the daemon restart the deploy triggered, before the reboot. This boot proves
the ordering and proves that a second pass over unchanged content rewrites
nothing. It leaves one property unproven: that a fresh write finishes before
the udev trigger reads a `.link` file. A boot with that directory empty would
prove it.

## The deploy prune against the daemon's files

Deleting a daemon-written file removes addressing from that provider link until
the daemon writes the file again, and the reload the prune notifies applies
that gap immediately. `deploy-mwan` prunes `/etc/systemd/network` by its own
list, and the shortened list omits the four files the daemon writes.

Controller, 2026-09-20 10:05, from main 429a6f62:

```bash
./configsctl deploy deploy-mwan --limit mwan_suburban_servers --check --diff
```

Observed: `failed=0`, and the prune task reported deleting
`20-enwebpass0.link`, `20-enwebpass0.network`, `20-enmbrains0.link` and
`20-enmbrains0.network`.

The fix merged as 10846744 (#457). A second `find` task lists every file in
that directory containing the daemon's marker line, and the prune loop skips
those paths. Controller, from that branch:

```bash
./configsctl deploy deploy-mwan --limit mwan_suburban_servers --check --diff
```

Observed: `ok=159 changed=19 failed=0`, and the prune task skipped all twelve
files.

## Not yet proven

The four failure cases from MWAN-492 have not been rerun against a gateway
where the daemon owns these files: fallback in both families, a link absent at
boot, the AT&T 802.1X path, and a daemon restart with links up. Production
still runs the earlier release and has not taken this change.
