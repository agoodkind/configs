# Multiple downstream routers over BGP

MWAN serves N downstream routers. Each router peers with both MWAN speakers
over iBGP, learns its default routes from them, and announces its own LAN
prefixes back, so MWAN routes return traffic per router with no static
internal next hop. Adding router N+1 is configuration plus a deploy, never a
code change.

## Defect this replaces

MWAN assumes exactly one downstream router. The return route for the whole
internal prefix is static, written by the wan.routes module toward one
configured edge address, and the speaker's neighbor list carries bare
addresses with no identity, no import policy, and no use of anything a peer
announces. A second router has no way to receive return traffic for its own
LANs.

## Contract

1. **Config declares each router.** A per-router entry carries the router's
   name, its v4 and v6 addresses on the shared transit segment, and the v6
   prefix allocations it is allowed to announce. Group vars render the
   entries; the service inventory owns the addresses. Config load fails when
   allocations overlap across routers or fall outside the internal block.
   The internal block is /60 today; allocation sizes are the operator's
   arithmetic, and a single /128 is a legal allocation. IPv4 carries no
   allocations because every router source-NATs v4 onto its connected
   transit address.
2. **The agent enforces disjoint announcements.** A per-peer import policy
   accepts only prefixes inside that peer's declared allocation. An
   announcement outside it is not installed and raises an alert naming the
   router and the prefix.
3. **The agent owns the learned-route FIB.** A best-path watch installs
   accepted prefixes into the main table and every WAN table via the netif
   primitives, tagged with the BGP routing-protocol number so tooling and
   wan.routes distinguish them from static state. Installed routes are never
   torn down on agent stop and are re-reconciled on start, so a graceful
   restart moves no packets. A dropped session removes exactly that
   router's routes within the hold timer.
4. **Both speakers serve every router.** Each router peers with the MWAN VM
   and the failover LXC. The LXC runs the same receive path into its main
   table, so return traffic to every router flows during failover.
5. **The static internal route retires behind a shadow flag.** In shadow
   mode the static route stays authoritative while the agent logs intended
   installs for parity. The authoritative flip removes the static route.
   Testbed first; the production flip needs an explicit operator go.
6. **Routers announce what they own.** Every router announces its declared
   prefixes over iBGP in AS 4200000001. For OPNsense that is FRR network
   statements per family; the owner of that router-side configuration is
   the MWAN-276 decision.

## Boundaries

- NPT is out of scope. It keeps translating the whole internal block per
  WAN, so allocations inside the block translate unchanged. A future /56
  internal block requires delegations that carry it.
- The watchdog is out of scope. Router loss is handled by the hold timer
  withdrawing that router's prefixes; VM failover already covers the
  defaults direction.
- Same-prefix announcements from two routers are rejected by contract 2.
  The active-standby same-prefix pattern exists only between the two MWAN
  speakers toward each router, resolved by the router-side local-preference
  policy that runs today.

## Acceptance criteria

- AC1: A testbed router-2 simulator is onboarded by config alone: peer
  entry plus allocation, deploy, and its LAN prefix routes appear in the
  main and WAN tables on the VM and in the main table on the failover LXC.
- AC2: Traffic sourced behind router-2 egresses a WAN and returns through
  router-2, while OPNsense-testbed traffic is unaffected.
- AC3: An announcement outside the simulator's allocation is not installed
  and raises the alert naming router and prefix.
- AC4: Killing the simulator's BGP session removes its routes within the
  hold timer and leaves every other router's routes in place.
- AC5: During a forced VM failover, router-2 return traffic flows through
  the failover LXC.
- AC6: An agent restart with graceful restart enabled leaves the learned
  routes installed throughout; no probe loss attributable to route churn.
- AC7: Shadow mode shows parity between the static internal route and the
  intended learned installs before any authoritative flip.
