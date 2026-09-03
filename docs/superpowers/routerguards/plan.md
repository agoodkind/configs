# Router guards implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development to implement this plan slice by
> slice. Each slice is one reviewed change.

**Goal:** Refuse a web-interface firmware upgrade when the router's virtual
hardware has drifted, and open a change window on every deliberate router
reboot so the watchdog stops blaming the gateway and can recover the router.

**Architecture:** Two guards on the guest's own hook stages, one declared
baseline in the service mapping, one drift timer and one extended watchdog on
the hypervisor.

**Tech Stack:** FreeBSD hook scripts calling the monolith's FreeBSD build, Go
for the daemon and watchdog, Ansible for install, Proxmox snapshots for
recovery.

**Spec:** [spec.md](spec.md)

## Global constraints

- Do not change any documented transport invariant. The new event is one more
  consumer of the existing channel: no raised write cap, no liveness frame, no
  changed acknowledgement pacing, no character device close.
- New tools are subcommands of the one binary, never separate binaries. A hook
  script is a thin shim that calls a verb.
- Every input value is declared in group vars, the service mapping, or
  OpenTofu, and read bare. Reading it defensively is banned and linted.
- The router guest has no MWAN configuration directory; its settings are
  FreeBSD run configuration entries.
- Snapshots carry no saved memory, and their names stay under forty
  characters because Proxmox truncates past that silently.
- Go work runs through the repository's make targets, never raw Go.
- Units and files reach hosts through Ansible, never hand-edited.
- Testbed first, production second, and never both hypervisors in one command.

## 1. Declare the baseline

Add the router's per-device record to the service mapping: slot, bridge, and
hardware address per device, for the production and testbed routers. Render
the hardware addresses onto each guest as a run configuration entry beside the
daemon's existing one, and the whole record onto each hypervisor for the drift
timer. Leave the OpenTofu resource alone, including its ignored network
devices, and correct its comment to name the service mapping as the baseline's
home.

Acceptance: an OpenTofu plan against production comes back empty, and both
guests and both hypervisors carry the rendered baseline after a deploy.

## 2. Refuse an upgrade on drift

Add a verb to the FreeBSD build that reads the rendered baseline, reads the
guest's own interface hardware addresses, and exits non-zero when the set or
the count disagrees, printing what it found against what it expected. A
missing or unreadable baseline is a refusal. A documented override marker
turns the refusal into a warning and a zero exit. Install it as the guest's
upgrade-stage hook through the same task that installs the daemon's other
guest files.

Acceptance: AC1, AC2, AC3, and the hook half of AC4.

## 3. Detect drift from the hypervisor

Add a timer unit on each hypervisor that reads every guarded guest's live
device list the way the existing runtime-network discovery task does, over
SSH and never the Proxmox HTTP API, diffs each device's slot, bridge and
hardware address against the baseline, and raises one notifier alert per
guest on any difference. Detection only; it changes nothing on any guest.

Acceptance: AC5.

## 4. Carry a guest event to the hypervisor

Add a server-streaming subscription to the serial service that the guest
pushes events into, and a verb the guest calls locally to publish one. The
host bridge opens that subscription per session using the same session dialer
its heartbeat already uses, and resubscribes when a session is rebuilt. On
receiving a router shutdown event the bridge records an open change window on
the hypervisor where the watchdog reads it. Keep the event small enough that
it never approaches the channel's write cap.

Acceptance: the bridge records a window within seconds of a testbed guest
publishing the event by hand, and AC8 holds when the bridge is restarted
mid-arm.

## 5. Arm the window and cover the router

Install the guest's stop-stage hook, which publishes the shutdown event and
returns within a short bound, treating any failure as permission to continue
the reboot. Teach the watchdog a second guarded guest: take healthy snapshots
of the router on the terms it already uses for the gateway, suppress gateway
rollback while a router change window is open, and roll the router back to its
newest healthy snapshot when the window closes with egress still lost. Extend
the watchdog's existing tests for the new decision branch, since that branch
has no coverage today.

Acceptance: AC6, AC7, and the stop-hook half of AC4.

## 6. Correct the documentation this changes

The watchdog page documents one guarded guest and exactly two rollback
signals, so rewrite it for the second guest and the third signal. Extend the
router operations rule on hot-added devices with the consequence this incident
found, that inert devices renumber every interface on the next boot. Correct
the layout page, which says only the testbed hypervisor runs the serial
bridge while production runs it too. Add the drift timer to the production
hypervisor's service list.

Acceptance: each page states current behavior, and no page repeats a fact that
another page owns.

## Self-review

Slices 1, 2, 3 and 6 deliver the prevention and depend on nothing in 4 or 5.
Slice 5 depends on slice 4's event path and on slice 1's baseline only for the
guest hook it shares with slice 2.

The spec's AC9 is residual by construction: the production router's device
count differs from the testbed's, so the production half of the baseline is
proven by the first production drift check rather than by any testbed run.
No slice claims to close it.

One known gap is carried deliberately. Slice 5 gives the router a rollback for
a deliberate reboot that breaks egress. A router that fails on its own still
produces the misattribution this incident showed, because the watchdog's
evidence is unchanged in that case. That is the separate watchdog work, and
this plan does not close it.
