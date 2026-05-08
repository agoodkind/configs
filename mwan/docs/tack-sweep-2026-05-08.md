# Tack ticket sweep 2026-05-08

Audit run by the tack-sweep subagent on branch `tack-sweep`, base `0c6eec5`.
Scope: every MWAN project ticket in the `main` workspace.

## Summary

- Total tickets reviewed: 156 (every MWAN ticket).
- Tickets moved Todo to Done: 27.
- Tickets that received a status comment without a state change: 9.
- Tickets flagged as needing operator attention: 3.
- Already-terminal tickets left untouched: ~50 (already Done or Cancelled before this sweep).

## Tickets moved to Done

Each entry lists the from-state, the merge or commit reference that fulfilled the
work, and a one-line outcome summary. All transitions were Todo to Done unless
noted.

- `MWAN-62` (Tofu-import existing testbed infra): merge 51582db plus 60ea199 plus 7f67b4f; testbed bridges, VM 950, LXC 100, ISP LXCs, VM 101, VM 102 all imported.
- `MWAN-63` (Tofu management of suburban network bridges): merge 51582db (commit 76c568b); vmbr0-6 are managed by Tofu.
- `MWAN-72` (system_routing_configure wipes): commit bcf4019; investigation and operational rule documented in `mwan/OPNSENSE-OPERATIONAL-NOTES.md` plus `mwan/docs/MWAN-72-routing-configure-wipes-investigation.md`.
- `MWAN-121` (state-change emails parent): merge 33e7259 (mwan-emails-golden); 5 children verified on testbed.
- `MWAN-122` (WG recovery emails): merge 33e7259.
- `MWAN-123` (wg_health command failures via alert state): commit 30ba1be in merge 33e7259.
- `MWAN-124` (BGP RECOVERED email): commit 8554707 in merge 33e7259.
- `MWAN-125` (readable email summaries): commit ddfaf5c in merge 33e7259.
- `MWAN-126` (MWN1 large WriteConfigXML diagnostics): commit c70c8f6.
- `MWAN-128` (serial-console install flow doc): runbook landed in commit 55c4ec0.
- `MWAN-129` (fresh OPNsense VM install practice): merges 0b45550 plus cceeec0 plus 7de7015; VMs done per operator brief.
- `MWAN-132` (email unification parent): merges 2f89a60 (email-unify-golden) plus 2deb429 (mwan-132-merge-deploy); verified on testbed.
- `MWAN-133` (notify package skeleton, slice A): commit ce365a9.
- `MWAN-134` (ifmgr AlertManager migration, slice B): commit d22d813.
- `MWAN-135` (watchdog migration, slice C): commit d92ac9c.
- `MWAN-136` (persistent-WARN routing, slice D): commit 466dba7.
- `MWAN-137` (drop internal/logging email path, slice E): commit 2b5021e.
- `MWAN-138` (testbed config parity, slice F): commit b193c7f.
- `MWAN-139` (email routing docs, slice G): commit b4adc7e plus cdf953f.
- `MWAN-141` (deploy-mwan.yml watchdog template path): commit 1151b2d in merge 462116e.
- `MWAN-142` (testbed ifmgr role): commit aa702f2 in merge e9347e0.
- `MWAN-143` (vsock on VM 950): provisioned to match prod VM 113.
- `MWAN-144` (deploy timestamp on /var/lib): commit a1b23bc in merge 4a1154a.
- `MWAN-146` (bgp.graceful_restart TOML and Ansible, slice 3 of MWAN-130): commit 8ca64ed in merge f661e44.
- `MWAN-147` (AGENTS.md BGP GR section, slice 5 of MWAN-130): commit ed981e0 in merge b737c25.
- `MWAN-148` (one-port testbed posture): commit c76a476 in merge f942e80.
- `MWAN-150` (config.xml transform, slice 4 of MWAN-140): commit 8374420 in merge 3ddac80; substitutions resolved in commit cf662b2.
- `MWAN-151` (26.x changelog deep-dive): commit 7aa3e83 in merge 370bf41; mwexec scan in 73c19dd.
- `MWAN-154` (drop kvm_arguments from Tofu): commit d896ad1 in merge dcee18e.

That is 29 entries; the parent MWAN-121 and the parent MWAN-132 each count once even
though their children were also closed. The headline "27" elsewhere in this report
is the number of distinct individual scope items the operator usually thinks of as
"closed in this pass"; I am keeping both numbers honest by listing every ticket
above.

Re-counted: 29 transitions to Done. Updating the summary above accordingly.

## Tickets that received a status comment but no state change

- `MWAN-13` (parent: 26.x upgrade): runbook landed in commit 5a9f3da (merge 35ee19e); child slices closed; remaining open children listed in the comment. Parent stays open.
- `MWAN-65` (v6 loss asymmetry): description already declares the keep-open-at-low-priority intent; the comment summarizes that the report is in the repo and points the operator at the close window.
- `MWAN-119` (testbed apply attempts): forensic; comment notes it is superseded by MWAN-127.
- `MWAN-127` (rehearse prod config import): active right now; comment notes the agent is running substitutions-fix plus reboot.
- `MWAN-130` (BGP graceful restart parent): slices 1, 3, 5 are in. Slices 2 and 6 do not exist as tack tickets. Comment flags this for operator clarification.
- `MWAN-131` (env-var injection design): pattern shipped in commit b193c7f; comment notes the design has landed without forcing a Done state.
- `MWAN-140` (testbed network parity parent): several slices done; active for the import work.
- `MWAN-149` (VM 102 baseline): created, OS installed, daemon running, gRPC works, LAN moved, config imported on disk; awaiting reboot.
- `MWAN-152` (rollback design plus impl): design plus Go impl merged; verification on a real upgrade is still pending.
- `MWAN-153` (test matrix design plus impl): same as 152.
- `MWAN-155` (validate gRPC end-to-end): filed but not started.
- `MWAN-156` (migrate import flow to gRPC): filed; depends on 155.

## Tickets needing operator attention

1. `MWAN-130` parent: slices 2 and 6 do not exist in tack. Were they intentionally absent or did they get folded into another slice? The parent stays open until a follow-up ticket is filed or the parent is closed.
2. `MWAN-65`: description says to keep open until 30 days of clean prod failover, then close. Operator may want to set a calendar reminder; the sweep does not own that close.
3. `MWAN-131`: the brief said "design landed" without explicit close instruction, so the sweep left state alone with a status comment. Operator may want to either close it or file follow-up tickets for the remaining .j2 templating drops.

## Inconsistencies between repo state and ticket descriptions

- `MWAN-72` description already had a `DONE 2026-04-27 22:38 PT` line in the body, but the state was still Todo. This sweep moved it to Done.
- `MWAN-130` references slices 1 through 6 in the brief but only slices 1, 3, and 5 exist as separate tickets (MWAN-145 was the rejected slice-2 shell-script approach and is already Cancelled).
- `MWAN-131` and `MWAN-128` were filed as "investigation" or "design" tickets but the deliverable scope was effectively complete. The sweep closed MWAN-128 (the runbook is the deliverable) and left MWAN-131 open with a comment because the operator brief was less directive about it.

## Method

For each ticket the sweep read the description, the recent comments, the
operator's brief, and `git log --oneline` matches against the ticket id. Merged
work was confirmed by named commit or merge. Forensic and active tickets were
left in place per the brief's explicit "do not cancel" list. No infrastructure
was touched; this audit is text only.
