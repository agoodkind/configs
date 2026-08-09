# Multiple downstream routers over BGP

MWAN serves N downstream routers. Each router peers with both MWAN speakers
over iBGP, learns its default routes from them, and announces its own LAN
prefixes back, so MWAN routes return traffic per router with no static
internal next hop. Adding router N+1 changes nothing on MWAN: the new
router peers over the transit link and announces what it routes.

## Defect this replaces

MWAN assumes exactly one downstream router. The return route for the whole
internal prefix is static, written by the wan.routes module toward one
configured edge address, and the speaker uses nothing a peer announces. A
second router has no way to receive return traffic for its own LANs.

## Contract

1. **Config declares the transit link, not the routers.** The speaker
   accepts a BGP session from any address on the internal transit segment,
   as dynamic neighbors on the transit v4 net and the transit v6 /64,
   alongside the statically configured sessions that exist today. Bringing
   up router N+1 requires no MWAN config change and no deploy.
2. **The agent installs what routers announce.** Neighbors are trusted to
   announce only what they route, the way eBGP peers are trusted; the
   speaker applies no import filter, and how a router subdivides its own
   space is its business. IPv6 unicast best paths learned from peers
   install into the kernel; locally originated announcements, the
   defaults, never do. When two routers announce the same prefix, BGP
   best-path selection picks the next hop, the same mechanism that
   resolves the two speakers' identical default announcements on the
   router side today. IPv4 return routing is unchanged, because every
   router source-NATs v4 onto its connected transit address.
3. **The agent owns the learned-route FIB.** A best-path watch installs
   learned prefixes into the main table and every WAN table via the netif
   primitives, tagged with the BGP routing-protocol number so tooling and
   wan.routes distinguish them from static state. Installed routes are never
   torn down on agent stop and are re-reconciled on start, so a graceful
   restart moves no packets. A dropped session removes exactly that
   router's routes within the hold timer; the installer tracks which peer
   owns each installed prefix. Stale-route cleanup arms only after a
   startup grace period, so a restart cannot sweep live routes before the
   sessions repopulate them.
4. **Both speakers serve every router.** Each router peers with the MWAN VM
   and the failover LXC. The LXC runs the same receive path into its main
   table, so return traffic to every router flows during failover.
5. **The static internal route retires behind a shadow flag.** In shadow
   mode the static route stays authoritative while the agent logs intended
   installs for parity. The authoritative flip removes the static route.
   Testbed first; the production flip needs an explicit operator go.
6. **Routers announce what they own.** Every router announces its own
   prefixes over iBGP in AS 4200000001. For OPNsense that is FRR network
   statements per family, applied by hand through the GUI (over the
   documented SSH forward on the testbed) or through the serial daemon,
   followed by the Quagga template reload and an FRR restart.

## Acceptance criteria

- AC1: A testbed router-2 simulator is onboarded with no MWAN change: the
  simulator peers over the transit link, and its LAN prefix routes appear
  in the main and WAN tables on the VM and in the main table on the
  failover LXC.
- AC2: Traffic sourced behind router-2 egresses a WAN and returns through
  router-2, while OPNsense-testbed traffic is unaffected.
- AC3: A prefix the simulator adds to its announcements installs within
  seconds, and withdrawing it removes the route without touching any
  other router's routes.
- AC4: Killing the simulator's BGP session removes its routes within the
  hold timer and leaves every other router's routes in place.
- AC5: During a forced VM failover, router-2 return traffic flows through
  the failover LXC.
- AC6: An agent restart with graceful restart enabled leaves the learned
  routes installed throughout; no probe loss attributable to route churn.
- AC7: Shadow mode shows parity between the static internal route and the
  intended learned installs before any authoritative flip.
- AC8: Every OPNsense config change cycle on the testbed runs the
  every-change gate: pre-change snapshot without saved RAM, the change,
  simulated traffic proof, then reset to the snapshot until the change is
  proven. The router config change is the brittle step and never lands
  unproven.
- AC9: A serial-channel recovery drill passes on the testbed before any
  production change: with the router's network path treated as dead, the
  FRR announcement change is fully reverted over the out-of-band serial
  daemon alone (config write, Quagga template reload, FRR restart), and
  the router returns to the pre-change routing state.


## Boundaries

- NPT is out of scope. It keeps translating the whole internal block per
  WAN, so router space inside the block translates unchanged. A future /56
  internal block requires delegations that carry it.
- The watchdog is out of scope. Router loss is handled by the hold timer
  withdrawing that router's prefixes; VM failover already covers the
  defaults direction.
- Import filtering is deliberately absent. Routers are trusted to announce
  only what they serve; a router announcing space it cannot deliver is a
  router-side defect, not something MWAN polices. The active-standby
  same-prefix pattern between the two MWAN speakers stays resolved by the
  router-side local-preference policy that runs today.
- Router-side peer configuration is out of scope. Each router configures
  its own sessions toward the two speakers. Direct router-to-router
  peering is not needed, because inter-router traffic transits mwan on
  the learned routes.
- Automating the router-side BGP config is out of scope. The FRR
  announcement change stays a manual exercise; who eventually owns pushing
  that config is the separate MWAN-276 decision and gates nothing here.
