# Guard the router against hardware drift and misattributed rollback

## Goal

An OPNsense firmware upgrade started from the web interface refuses to run
when the router's virtual hardware has drifted from its declared shape, and
any deliberate router reboot opens a window on the hypervisor that stops the
watchdog blaming the gateway and lets it recover the router instead.

## Defect this replaces

On 2026-08-01 the hypervisor API added two virtio NICs to the router guest
under the OpenTofu provider's token. Nothing on either host compares a guest's
live hardware against its declared shape, and a hot-added NIC binds nothing
until reconfigured, so the drift stayed invisible for thirty-two days. On
2026-09-02 an operator upgraded the router firmware from the web interface.
That was the guest's first boot carrying four NICs. FreeBSD numbered them by
PCI slot, so the transit link landed on the LAN bridge. No BGP session
formed, and the router held no default route in either family.

Two guards existed and neither helped. The router's own guarded upgrade path
snapshots the guest and validates BGP sessions and kernel default routes at
blocker severity, but it is a command line verb and the upgrade started in a
browser, so it never ran. The watchdog did run. Its only evidence is its own
egress probes through that router, so it found a gateway deploy seven minutes
old inside its window and restored a healthy gateway to a pre-deploy
snapshot. Production lost egress for thirteen minutes, and the gateway lost a
cutover that had already passed its verdicts.

## Contract

1. **The upgrade refuses on drift.** A hook on the guest's upgrade stage
compares the interfaces FreeBSD sees against a declared baseline and exits
non-zero when they disagree. OPNsense then aborts the upgrade before the
kernel apply and before any reboot, and the hook's reason reaches the
operator in the firmware log the web interface already streams.

The question the hook answers is whether this reboot will renumber the guest,
and only the hypervisor holds that answer. FreeBSD assigns interface unit
numbers as devices attach, in slot order at boot, and never renumbers a
running system. So the guest's live order records the slot order it booted
with, while the hypervisor's current configuration determines the order it
will boot into next. The hook refuses when those two disagree.

The hypervisor is the one that knows, and it dials the guest rather than the
other way round. So the hook asks for an answer and refuses unless one comes
back, inside a bound. A cached answer will not do: one can sit younger than
any staleness bound and still predate the change that matters, and a bound
alone would make the dead-channel case depend on how recently the channel
died rather than on whether it is dead.

Each request carries an identifier the answer must echo, and the hook accepts
only an answer bearing the identifier it sent. Ordering by time would not do
either: it compares two clocks nobody synchronised, and it lets a late answer
to a request that already timed out satisfy the next hook run, which is a
stale verdict wearing a fresh timestamp.

The hook refuses on either of two disagreements, because a reboot is when
latent drift detonates and it is the last cheap moment to catch it. The
guest's live order disagreeing with the hypervisor's current slot order means
this reboot renumbers. The hypervisor's own configuration disagreeing with the
declared baseline means hardware nobody declared is present, which is the
condition that sat unnoticed for thirty-two days before the outage. The second
is not always the first: a device appended after the existing slots renumbers
nothing, and still must not survive into a reboot unexamined. The hypervisor
holds both facts, so it answers with both.

A copy of the declared baseline rendered onto the guest cannot answer this.
That copy is written at deploy time, so a slot change made afterwards leaves
the stale copy and the live order agreeing, and the guard passes the very
change it exists to catch. The 2026-08-01 write was caught by a count only
because a hot-added device is visible to the running kernel; two devices
exchanging slots are not.

The guest cannot see bridge membership, and it is not the authority on what
was declared. Those belong to the host-side check.

2. **The refusal fails closed and can be overridden.** An answer the hook
cannot obtain is a refusal, stated as such, including one it cannot obtain
because the channel is down. An operator who has decided the guard itself is
the broken thing creates a documented marker file, and the hook then logs
loudly and permits the upgrade. Refusing on a dead channel is deliberate: the
hook cannot tell a quiet hypervisor from a renumbering one, and a postponed
upgrade is recoverable where a renumbered router is not.

3. **The declared baseline has one home.** The per-device record lives in the
service mapping, which already owns pinned hardware addresses. Both consumers
read it on the hypervisor, where the live configuration it is compared against
also lives: the drift timer reports a difference continuously, and the upgrade
answer carries the same comparison at the moment it matters most. Nothing is
rendered onto the guest, which is neither the authority on what was declared
nor able to see what the hypervisor holds now.

OpenTofu keeps ignoring this guest's network devices: its provider models
network devices as a sequential list, the guest's devices are not sequential,
and that mismatch is what wrote the placeholder NICs in the first place.

4. **Drift surfaces within the hour.** A timer on the hypervisor reads each
guarded guest's live device list and alerts through the existing notifier on
any difference from the baseline, whatever caused it. It reads the guest
configuration the way playbooks already do and never calls the Proxmox HTTP
API.

5. **A deliberate reboot opens a router change window.** A hook on the guest's
stop stage sends one event to the hypervisor before the guest goes down. That
stage runs for every deliberate reboot and shutdown whatever started it, and
does not run on a panic or a power cut, so a crash still presents as a fault.
The event travels as a server-streaming subscription on the existing serial
service, which the host bridge holds open per session beside its heartbeat,
and the bridge records the window on the hypervisor where the watchdog reads
it.

6. **The window protects the gateway and covers the router.** While a router
change window is open the watchdog does not roll the gateway back for lost
egress. If egress has not returned when the window closes, it rolls the
router back instead. The rollback target is the newest healthy snapshot the
watchdog itself took of the router, on the same terms it already takes them
for the gateway, because a snapshot taken once the guest is already shutting
down would capture a staged upgrade rather than the state before it.

7. **The hooks never wedge their caller, and they fail in opposite
directions.** The hook dispatcher runs each hook synchronously with no
timeout of its own, so each hook bounds its own work. There the two part
company. The upgrade-stage hook treats every failure as a refusal, because
refusing costs a postponed upgrade while proceeding blind costs the router.
The stop-stage hook treats every failure as permission to continue, because
losing a window costs a guard while a hook that hangs costs the reboot.

8. **The window is armed before the guest goes down, or not at all.** The
stop-stage hook waits, inside its own bound, for the hypervisor to confirm
the window is recorded. Without that confirmation it proceeds unprotected and
says so. It does not persist the event across the reboot and does not retry
without bound: a queued retry on the shutdown path is a guest-to-host write
held open exactly when the channel documents a wedge, and after the reboot
the window it would have opened is moot.

## Boundaries

The transport invariants stay exactly as they are. The new event is one more
consumer of the existing channel, never a reason to raise the write cap, add a
liveness frame, change the acknowledgement pacing, or close the character
device.

OpenTofu does not gain management of this guest's network devices.

The command line upgrade path keeps its own snapshot, validation matrix and
rollback. This work guards the reboot rather than replacing that tool, and
does not require an operator to use it.

Drift is reported, never repaired. A hot-added device binds nothing until
reconfigured, so automatic removal is its own foot-gun.

A router that fails on its own rather than deliberately stays out of scope and
belongs to the separate work on teaching the watchdog to check the router
before blaming a gateway deploy.

## Acceptance criteria

AC1: with a spare device added to the testbed router after its existing slots
and no reboot taken, an upgrade started from that router's web interface
aborts naming undeclared hardware, the firmware log carries the hook's
reason, and the guest still runs its previous firmware. This placement
renumbers nothing, so the refusal comes from the declared comparison rather
than the order one.

AC2: with the spare device removed, the same upgrade proceeds and completes.

AC3: with the spare device present and the override marker in place, the
upgrade proceeds, and the hook's warning appears in the firmware log.

AC4: both hooks are present and executable on the guest after a completed
firmware upgrade.

AC5: adding a device to a guarded testbed guest raises one alert within a
single timer interval naming the guest and the difference, and removing it
clears the alert.

AC6: rebooting the testbed router from its web interface records a change
window on the hypervisor before the guest stops, and the same reboot taken
while a testbed gateway deploy sits inside its own window leaves the gateway
untouched.

AC7: with the testbed router's transit link broken so egress does not return,
the watchdog rolls the router back to its newest healthy snapshot, and the
post-rollback checks the router runbook already lists all pass.

AC8: restarting the host bridge while a testbed router reboot is arming leaves
both guest processors idle rather than one polling a virtual queue and one
delayed on a lock, and the router completes its reboot.

AC9 (residual, production topology only): both routers carry two devices, but
the testbed router occupies consecutive slots while the production router
occupies the first and fourth. The drift check's handling of a
non-consecutive slot list is therefore proven only by the first production
run after this ships.

AC10: with the testbed router's two devices exchanged between their slots and
no reboot taken, an upgrade started from that router's web interface aborts
and names the reordering. Nothing changes inside the running guest in this
case, so neither a set nor a count nor any guest-local copy of the baseline
would have caught it.

AC11: with the host bridge stopped, a testbed router reboot still completes in
its normal time, and the hook reports that it could not confirm a change
window.

AC12: with the host bridge stopped, an upgrade started from the testbed
router's web interface aborts, naming the unreachable hypervisor as the
reason, and proceeds when the override marker is present. The result does not
depend on how long ago the bridge stopped.

AC13: with the testbed router's two devices exchanged between their slots at
the hypervisor and the declared record edited to match, and no reboot taken,
an upgrade still aborts. Declaring a layout does not make a guest that has
not booted into it safe to reboot, and this is the case that separates the
two comparisons: the declared one now passes while the order one refuses.

AC14: with a device appended after the testbed router's existing slots and
the declared record edited to match, an upgrade proceeds. Appending renumbers
nothing, so a refusal here would be a false one.
