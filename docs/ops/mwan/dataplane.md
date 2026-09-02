# MWAN data plane

Three `mwan ifmgr` modules in the `mwan-ifmgr@wan` unit converge the data
plane independently of the BGP speaker: `wan.routes` owns `ip rule` and the
per-WAN routing tables, `health` owns the WAN health verdicts and state
files, and `npt` owns the runtime `table ip6 nat`. A health transition
requests an immediate reconcile in-process, so a failed WAN reroutes without
waiting for a periodic tick.

## Shared per-WAN foundation

`wan.routes` and `npt` run in the `wan` role and read one WAN list from the
gateway's network configuration, which the daemon loads from
`/etc/mwan/network.json` and validates against the model before it programs
anything. Each WAN entry hangs off the interface that carries it and holds that
WAN's full config: its name plus the policy-routing slots `wan.routes` programs
(table, fwmark, priorities, prefixes). The internal prefix and edge addresses
both modules translate against are group-wide values in the same file. Each
module reads the fields it needs from that one per-WAN entry, so each WAN has a
single home instead of a per-module list matched by name.

## wan.routes ifmgr module

The `wan.routes` module owns policy routing. It watches each WAN interface
over netlink and reconciles the per-WAN tables and the `ip rule` set on every
default-route change plus a periodic tick, so it does not miss a late
RA-learned default route. It owns priorities 50/55/56/57/100/200/300. Its
fwmark rules share a priority with the networkd from-edge rules without
thrash.

## npt ifmgr module

The `npt` module owns the runtime `table ip6 nat`. It runs as a second module
in the `mwan-ifmgr@wan` instance alongside `wan.routes` and self-disables when
the network configuration lists no WANs.

npt derives every WAN's `/60` from the live DHCPv6-PD delegation on that WAN's
interface, with no static fallback. A WAN with no delegated prefix is skipped for
that reconcile and alerted, rather than translated against a guessed prefix.

npt owns the `prerouting` and `postrouting` chains and replaces each chain's
full rule set in one atomic `google/nftables` transaction, so no packet sees an
empty chain mid-update and no reconcile can leave a duplicate rule. An nft
watcher requests a reconcile when the `ip6 nat` table changes underneath the
module, so an external `nftables` reload converges back within one pass.

npt does not tear its rules down when the module stops. A binary swap or an
`mwan-ifmgr@wan` restart leaves the kernel forwarding on the last-applied rules,
and the next reconcile after restart re-applies them atomically. Forwarding never
stops, and no swap leaves a chain empty or double-programmed.

## Health state persistence and email guard

The `health` module keeps two state files:

- **Runtime state** at `/var/run/mwan-health.state`, the file `--status`
  consumers read.
- **Persistent state** at `/var/lib/mwan/health-state`, the module's memory of
  last-known WAN states across daemon restarts.

On start, the runtime file is seeded from the persistent file when it exists,
and only WANs missing from the persistent file get `unknown`. Both files are
written atomically on every verdict change.

The persistent seed keeps restarts honest: a brand-new host has no persistent
file, so first-ever transitions read `unknown -> X` and email correctly stays
off, while every subsequent restart sees the prior state and emails real
transitions.

Failure modes worth knowing:

- **Empty `table ip6 nat`** means runtime programming did not happen or was
  flushed. The npt module's nft watcher reconverges it within one reconcile;
  `systemctl restart mwan-ifmgr@wan` forces the pass immediately.
- **Boot ordering**: PCI/virtio devices and AT&T 802.1X authentication can be
  late. The ifmgr modules replay on netlink events and periodic ticks, so a
  late device converges without a dispatcher-style one-shot race.

For terminology, prefer **healthy / unhealthy / unknown** for WAN state. Avoid
**up / down** for health, because that conflicts with `ip link` administrative
state.

## Tracing

The ifmgr daemon emits structured JSON logs to `/var/log/mwan-ifmgr.jsonl`,
and each line carries the active `traceId` so events across a deploy or a
boot correlate. `update-att-pinned-dests.sh` still writes shell-side JSON to
`/var/log/mwan-debug.log` when `mwan_debug_logging: true`.

Trace ID sources:

- `mwan-trace-boot.service` writes `/run/mwan-trace-id` and
  `/var/lib/mwan/trace-id` at boot.
- The deploy playbook writes the same files at the start of deploy.

Quick check on MWAN:

```bash
cat /run/mwan-trace-id
mwan debug trace-tail
```

## Inspect the data plane

On MWAN:

```bash
ssh root@mwan.home.goodkind.io
wpa_cli status
systemctl status wpa_supplicant-mwan systemd-networkd networkd-dispatcher \
  nftables mwan-ifmgr@wan cloudflared
mwan debug npt              # native inspection: prefixes|routes|policy|status|stats|sim4|sim6|npt
mwan debug connectivity     # active probes: ping4|ping6|curl4|curl6|lb4|lb6|lb4-ifaces|lb6-ifaces
mwan debug systemd          # failed/stuck units and slowest starters over D-Bus
mwan debug trace-tail       # follow the ifmgr daemon JSON log (/var/log/mwan-ifmgr.jsonl), filter by trace id
```

IPv6 sanity checks:

```bash
ip -6 route show table 100
ip -6 route show table 200
ip -6 rule show
nft -a list chain ip6 nat postrouting
nft -a list chain ip6 nat prerouting
```
