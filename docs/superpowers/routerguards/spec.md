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
until reconfigured, so the drift stayed invisible for thirty two days. On
2026-09-02 an operator upgraded the router firmware from the web interface.
That was the guest's first boot carrying four NICs, FreeBSD numbered them by
PCI slot, the transit link landed on the LAN bridge, no BGP session formed,
and the router held no default route in either family.

Two guards existed and neither helped. The router's own guarded upgrade path
snapshots the guest and validates BGP sessions and kernel default routes at
blocker severity, but it is a command line verb and the upgrade started in a
browser, so it never ran. The watchdog did run. Its only evidence is its own
egress probes through that router, it found a gateway deploy seven minutes
old inside its window, and it restored a healthy gateway to a pre-deploy
snapshot. Production lost egress for thirteen minutes and the gateway lost a
cutover that had already passed its verdicts.

## Contract

1. **The upgrade refuses on drift.** A hook on the guest's upgrade stage
compares the interfaces FreeBSD sees against a declared baseline and exits
non-zero when they disagree. OPNsense then aborts the upgrade before the
kernel apply and before any reboot, and the hook's reason reaches the
operator in the firmware log the web interface already streams. The guest
cannot see bridge membership, so it compares the set and count of `vtnet`
interface hardware addresses and nothing else.

2. **The refusal fails closed and can be overridden.** A baseline the hook
cannot read is a refusal, stated as such. An operator who has decided the
baseline itself is the broken thing creates a documented marker file, and the
hook then logs loudly and permits the upgrade.

3. **The baseline has one home.** The declared per-NIC record lives in the
service mapping, which already owns pinned hardware addresses. Ansible renders
the address half onto the guest and the whole record onto the hypervisor.
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

7. **The hooks never wedge the reboot.** The hook dispatcher runs each hook
synchronously with no timeout of its own. Each hook bounds its own work and
treats every failure as permission to continue. Losing a window costs a
guard; a hook that hangs costs the router.

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

AC1: with a spare device added to the testbed router and no reboot taken, an
upgrade started from that router's web interface aborts, the firmware log
carries the hook's reason, and the guest still runs its previous firmware.

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
