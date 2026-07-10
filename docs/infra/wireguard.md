# WireGuard endpoint roaming research notes

Sources read: `wireguard-go` (Jason Donenfeld's userspace reference impl,
~~/Sites/wireguard-go), `wireguard-tools` man pages (~~/Sites/wireguard-tools/src/man),
WG protocol description ([https://www.wireguard.com/protocol/](https://www.wireguard.com/protocol/)).

## What the protocol does

From `wg(8)` man page (canonical):

> Endpoint, an endpoint IP or hostname, followed by a colon, and then a port
> number. **This endpoint will be updated automatically to the most recent source
> IP address and port of correctly authenticated packets from the peer.** Optional.

This is enforced at the protocol layer. Every authenticated packet causes the
receiver to overwrite the peer's stored endpoint with the source IP and port of
the received packet. This includes handshake init, handshake response, transport
packets, and keepalives.

Source path in `wireguard-go`:

- `device/receive.go:374`. Handshake init: `peer.SetEndpointFromPacket(elem.endpoint)`.
- `device/receive.go:401`. Handshake response: same.
- `device/receive.go:459`. First authenticated transport: same.
- `device/receive.go:515`. Every subsequent authenticated transport: same.
- `device/peer.go:279-287`. `SetEndpointFromPacket` locks endpoint and overwrites
`peer.endpoint.val` with the new endpoint.

There is a `disableRoaming` flag at `device/peer.go:32`. It cannot be set via
standard config. No standard `wg`/`wg-quick` config keyword sets it. It is only
set true via private API `device.DisableSomeRoamingForBrokenMobileSemantics()`
at `device/mobilequirks.go:9`, or via the internal uapi field `brokenRoaming`
at `device/uapi.go:263`. Neither path is exposed. The flag is designed for
"broken mobile semantics" on iOS/Android, not for general use.

The kernel module behaves identically. Same protocol. Same author.

## Why this becomes split-brain in our setup

Architecture facts:

- `home.goodkind.io` Cloudflare DNS-LB rotates between AT&T-side address
(`2600:1700:2f71:c80::1`) and Webpass-side address (`2604:5500:c271:be00::1`).
- Suburban dials `home.goodkind.io:51820` on a periodic basis. DNS resolution
happens per dial, not cached forever.
- VM 113 has DNAT rules for both AT&T and Webpass IPv6 addresses. Both forward
to OPNsense `:fe::2:51820`.
- VM 113 mangle prerouting: inbound iif sets ct mark, so reply egresses the
same WAN suburban dialed. This is DNS-LB symmetry. It works for general
traffic.
- VM 113 mangle prerouting also has a mod-2 random LB rule for OPNsense-initiated
outbound: `ip6 saddr :fe::2 ct state new mark set numgen random mod 2`.

Two flows that break consistency:

### Flow A: suburban dials, then OPNsense rekeys

1. Suburban resolves `home.goodkind.io`. Cloudflare returns AT&T address.
2. Suburban dials `[2600:1700:2f71:c80::1]:51820`.
3. VM 113 DNATs to OPNsense. OPNsense's wg sees endpoint `:7bf2`, suburban's
  public Comcast SLAAC.
4. Inbound iif rule on VM 113 marks the conntrack AT&T (`mark=1`).
5. OPNsense replies. Reply ct state established. Mark restored from ct. AT&T.
6. Suburban's wg sees authenticated reply with src `:c80::1` after SNAT
  reverse. `SetEndpointFromPacket` keeps suburban's stored endpoint at
   `:c80::1`. Consistent with what it dialed.

So far healthy.

1. Two minutes later, OPNsense initiates a rekey. The rekey-after timer
  fires. New conntrack from `:fe::2`. Mod-2 rule fires. 50% chance: mark 2
   (Webpass).
2. Reply egresses Webpass. Postrouting NAT rewrites src to `:be00::1`.
3. Packet arrives at suburban. Suburban's wg validates the packet. Key matches.
  Then it calls `SetEndpointFromPacket` with the new source `:be00::1`.
   Suburban's stored endpoint is now `:be00::1`.
4. Suburban's next periodic re-resolution of `home.goodkind.io` is irrelevant.
  wg only re-resolves the configured `Endpoint=` line at startup and on
    persistent-keepalive failure. Roaming-set endpoints do not get re-resolved.
5. From now on, suburban initiates to `:be00::1`. If Webpass IPv6 path has
  any flakiness, the WG session degrades silently.

### Flow B: peer IP renumber

Suburban's Comcast `:7bf2` /64 changes (SLAAC renumber, modem reboot, lease
rotation). Suburban's outbound packets now come from a new source IP.
OPNsense receives, authenticates, calls `SetEndpointFromPacket`. Its stored
endpoint for suburban now updates to the new IP. OPNsense-side stays
consistent.

But until the next OPNsense-initiated send (rekey or persistent-keepalive),
suburban-side has not received anything from OPNsense via the new-source
flow. Suburban-side endpoint for OPNsense is unchanged. Symmetric to flow A.

If during the renumber window Comcast's modem stateful firewall conntrack
maps the old src and new src differently, or upstream Webpass and AT&T reply
paths have different reachability to the new prefix, then split-brain occurs.
Each side validly sees its peer at a different IP. Each side's outbound
continues to that stored IP. Asymmetric routing kills the session.

### Flow C: VM 113 reboots or conntrack flush

If VM 113 mwan-router reboots, all flows from `:fe::2` become "ct state new"
again on next packet. The mod-2 random LB rule re-runs. If suburban's stored
endpoint at the time was `:c80::1` but the new mod-2 picks Webpass, OPNsense's
reply egresses Webpass. Suburban's roaming sets endpoint to `:be00::1`. Flow
continues asymmetric to what suburban DIALS via its `Endpoint=` config. One
cycle later if mod-2 picks AT&T again, endpoint moves back. Endpoint flapping
in lockstep with mod-2 randomness on every conntrack flush.

## What CAN'T fix it

- **Pinning OPNsense-initiated to AT&T at VM 113** (commit `e2e17a1` shipped
earlier). Does not help because most traffic is suburban-initiated.
OPNsense-initiated rekeys also exist, but they only hit `ct state new` in
the brief window after conntrack expiry. The existing flow is usually in
`ct state established` from suburban's prior dial. So the pin rule rarely
fires. Effectively inert in practice.
- **Disabling roaming**. Not exposed in stock wg/wg-quick uapi. Would require
custom-built wireguard-go. Not feasible.
- **Static `Endpoint=` on suburban config**. Defeats DNS-LB failover. Goes
against active-active intent.
- **Pinning inbound on VM 113 to a single WAN regardless of iif**. Defeats
DNS-LB symmetry. Breaks all DNS-LB-routed traffic, not just WG.

## Detection plus manual reconciliation

Run wghealth on BOTH sides. On endpoint mismatch (post-NAT-normalization),
alert. Operator restarts wg-quick on one side. Both sides converge.

Pros: zero protocol-level change, observable.
Cons: requires manual intervention. The whole point of WG is "set and forget."

## Implementation note for bidirectional wghealth

Today's `wg_health` ([mwan/go/internal/ifmgr/modules/wg/](../../mwan/go/internal/ifmgr/modules/wg/)) polls OPNsense via SSH.

Two paths can add the suburban side:

- **Local-exec mode**. Run `mwan-ifmgr` on suburban with a `suburban-wg` role
  and a wghealth instance that reads the local WireGuard interface. Each daemon
  emits per-peer logs from its own viewpoint. Cross-checking happens by log
  analysis or a correlation layer.
- **Remote SSH mode**. Extend wghealth to support multiple SSH targets. The
  vault daemon polls both OPNsense and suburban over SSH. `wg` is only readable
  via root or the `wg-quick` group.

Local-exec keeps each box responsible for its own observation. Remote SSH keeps
one daemon responsible for both viewpoints but requires credentials and read
permissions on each target.
