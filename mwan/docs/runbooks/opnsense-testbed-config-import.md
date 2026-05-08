# OPNsense Testbed Config Import Runbook

Created: 2026-05-07

This runbook captures the current MWAN-13 / MWAN-127 rehearsal rule for importing
the production-shaped OPNsense `config.xml` onto the suburban testbed.

## Core Rule

Do not keep layering candidate configs on top of the already-mutated testbed
state. Restore the candidate onto a wiped or freshly restored OPNsense testbed
baseline, then validate from that clean state.

Do not use MWN1/gRPC as the reload mechanism during config-import debugging.
Use direct console, QGA, or SSH for restore and reload. Test MWN1 only after the
router is stable again.

Use the serial console while reloads or reboots are in progress. If SSH drops,
continue observing through the console path instead of guessing whether the
system is hung.

## Every Change Gate

Run this gate for every config change, including small XML edits.

### 1. Scope Check

- Confirm the target is suburban OPNsense VM `101`.
- Confirm commands do not reference `vault`, production OPNsense, production VM
  `113`, or production LXC `116`.
- Confirm the intended hypervisor is `suburban`.

### 2. Fresh Baseline Capture

- Capture Proxmox VM state and QEMU config.
- Capture OPNsense `/conf/config.xml` hash and byte size.
- Verify QGA ping and guest hostname.
- Verify the serial console path is reachable.
- Verify SSH reachability only as a convenience signal, not as recovery proof.

### 3. Backup Before Mutation

- Take a Proxmox snapshot.
- Back up the raw `/conf/config.xml`.
- Record the pre-change config hash.
- Write down the rollback command before applying anything.

### 4. Apply Method

- Restore the candidate config onto a wiped or freshly restored OPNsense
  baseline.
- Do not use MWN1/gRPC for reload.
- Use direct console, QGA, or SSH for config restore and reload.
- Watch the serial console during reload or reboot.

### 5. During Reload

- Watch serial console output.
- Tail relevant OPNsense logs, especially `configd` and system logs.
- Record the exact command that hangs, exits, or drops connectivity.
- Do not call a reload hung without console or log evidence.

### 6. Post-Change Validation

- QGA works.
- Serial console is responsive.
- SSH is reachable.
- Web UI TCP `443` is reachable.
- DNS returns `NOERROR`.
- Basic `configctl` actions work.
- Interface device bindings match the expected testbed topology.
- Default routes and gateways are sane.
- pf rules load.
- NAT sanity checks pass.
- FRR/BGP state is sane if FRR is expected.
- MWN1 `Version` and `ReadConfigXML` work after the router is stable.

### 7. Failure Rule

- If any baseline check fails, stop.
- Roll back immediately.
- Re-run the entire baseline after rollback.
- Record the failing command, relevant logs, and active config hash.

## Related Tickets

- `MWAN-13`: OPNsense upgrade to 26.x with rollback wired into Go monolith.
- `MWAN-126`: Fix MWN1 WriteConfigXML large-payload timeout diagnostics.
- `MWAN-127`: Rehearse prod config import on wiped suburban OPNsense baseline.
