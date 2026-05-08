# Worktree cleanup 2026-05-08

Audit and cleanup of `.claude/worktrees/` from the main repo at `/Users/agoodkind/Sites/configs`.

## Summary

- Total worktrees before cleanup: 80 (1 main repo + 79 sub-worktrees, including
  `worktree-cleanup` itself).
- Worktrees removed: 38.
- Worktrees preserved as forensic per rule 4: 6.
- Worktrees flagged for review (NOT_MERGED, not forensic): 8.
- Worktrees skipped because of uncommitted changes (untracked or modified): 25
  (rule 5).
- Active session worktrees kept: 2 (`worktree-cleanup` itself and the main repo).
- Total worktrees after cleanup: 42 (1 main repo + 41 sub-worktrees).

`main` was at `78045a8` and `origin/main` matched after `git fetch origin`. All
removed branches had every commit reachable from `main`
(`git merge-base --is-ancestor <tip> main` succeeded and `git rev-list --count
<tip> ^main` returned `0`).

## Worktrees removed

Each row records the branch name and the final tip SHA at deletion time. The
worktree was removed first via `git worktree remove`, then the branch via
`git branch -D`.

| Branch | Final SHA |
| --- | --- |
| build-all-targets | 0fdadba |
| email-unify-golden | 2b5021e |
| mwan-120-subtask-a | d126363 |
| mwan-120-subtask-b | 30ba1be |
| mwan-120-subtask-c | 8554707 |
| mwan-120-subtask-d | 50deab6 |
| mwan-120-subtask-e | ddfaf5c |
| mwan-13-changelog | 7aa3e83 |
| mwan-13-rollback-design | 8f491d9 |
| mwan-13-test-matrix | b470a5f |
| mwan-130-bgp-gr-config | 8ca64ed |
| mwan-130-bgp-gr-docs | ed981e0 |
| mwan-130-bgp-gr-speaker | f0a4847 |
| mwan-132-merge-deploy | 2f89a60 |
| mwan-140-vm101-nics | 252d982 |
| mwan-140-vm102-baseline | 03b217a |
| mwan-140-xform-impl | 8374420 |
| mwan-148-prod-parity | c76a476 |
| mwan-150-questions | cf662b2 |
| mwan-152-questions | c7f2548 |
| mwan-153-questions | be6b085 |
| mwan-40-opnsense-monolith | 7a6a009 |
| mwan-bgp-gr-deploy | ef95267 |
| mwan-buf-proto-migration | e3b5bf7 |
| mwan-emails-golden | 28dadcf |
| mwan-mwn1-v2-integration | 11f49f6 |
| mwan-opnsense-prod-import-apply | d91ce1b |
| mwan-opnsense-prod-import-inventory | d91ce1b |
| mwexec-deprecation-scan | 73c19dd |
| notify-slice-a | ce365a9 |
| notify-slice-b-ifmgr | d22d813 |
| notify-slice-c-watchdog | d92ac9c |
| notify-slice-d-noise | 466dba7 |
| notify-slice-g-docs | cdf953f |
| opnsense-import-research | 9065d26 |
| upgrade-rehearsal-runbook | 78045a8 |
| v6-transit-investigation | f522f78 |
| vm102-lan-and-daemon | 302843e |

All 38 worktrees and their 38 branches were removed cleanly. Each worktree
removal succeeded before the corresponding `git branch -D`.

## Worktrees preserved as forensic (rule 4)

| Worktree | Branch tip | Reason |
| --- | --- | --- |
| mwan-119-testbed-apply | 4f8159f | rule 4: any `mwan-119-*` branch (MWAN-119 v1/v2 forensic record) |
| mwan-119-v2-apply | 4f8159f | rule 4: any `mwan-119-*` branch |
| mwan-130-bgp-gr-toggle | 06f312c | rule 4: rejected shell-script approach, cited in MWAN-130 thread |
| mwan-140-opnsense-rcconf | b0454c9 | rule 4: rejected FreeBSD rename |
| mwan-140-suburban-bridges | 3bcfb03 | rule 4: rejected Ansible bridges |
| mwan-143-testbed-vsock | e6728bc | rule 4: Ansible vsock superseded by Tofu |

## Worktrees flagged for review (NOT_MERGED, not forensic)

These have unique commits not reachable from `main` and are not on the
do-not-remove list. They were left in place for the user to decide.

| Worktree | Branch tip | Unique commits | Notes |
| --- | --- | --- | --- |
| mwan-152-impl | e82aa31 | 1 | likely active or unmerged work for MWAN-152 |
| mwan-153-impl | 2657ee0 | 1 | likely active or unmerged work for MWAN-153 |
| mwan-mwn1-dispatcher | adf660a | 3 | dirty (untracked `mwan/go/internal/mwn1/`); appears active |
| proxmox-args-privilege-research | 04f1ab6 | 1 | research branch |
| run-config-transform | 86e17a6 | 1 | research branch |
| vm102-config-import | b9aca48 | 1 | dirty (untracked generated dir) |
| vm102-manual-create-import | 2d65cef | 1 | dirty (untracked plan file) |
| vm102-os-install | 30b8839 | 1 | clean, but unmerged |

## Worktrees skipped because of uncommitted changes (rule 5)

These are MERGED into `main` but had untracked or modified files at audit
time. Per rule 5, skipped without removing. The user can clean them up
manually, or once their working tree is clean they can be removed in a
follow-up pass.

| Worktree | Branch tip | Status indicator |
| --- | --- | --- |
| mwan-126-mwn1-large-write | 4f8159f | modified `mwan/go/internal/mwn1/conn.go` |
| mwan-129-opnsense-serial-practice | 4f8159f | untracked runbook md |
| mwan-141-deploy-path-fix | 1151b2d | untracked `verify.txt` |
| mwan-142-testbed-ifmgr-role | aa702f2 | untracked `verify.txt` |
| mwan-144-deploy-timestamp | a1b23bc | untracked `verify.txt` |
| mwan-154-args-cleanup | d896ad1 | untracked `opentofu/plan-after-args-cleanup.txt` |
| mwan-62-testbed-tofu | 4f8aa42 | untracked `opentofu/validate.txt` |
| mwan-62-tofu-apply | 7f67b4f | untracked `opentofu/apply.txt` |
| mwan-62-tofu-import | ef95267 | untracked `opentofu/plan-after-import.txt` |
| mwan-62-tofu-reconcile | a10af7b | untracked `opentofu/plan-post-reconcile.txt` |
| mwan-mwn1-stash-salvage | 48dc1d4 | modified `mwan/go/internal/mwn1/conn.go` |
| mwan-mwn1-v2-bridge | 34dfc82 | modified `mwan/go/cmd/mwan-opnsense-host/main.go` |
| mwan-mwn1-v2-callers | 34dfc82 | modified `mwan/go/cmd/mwan-opnsense-host/proxy.go` |
| mwan-mwn1-v2-cancel | 34dfc82 | modified `ansible/playbooks/tasks/mwan-opnsense-host-deploy.yml` |
| mwan-mwn1-v2-client | 34dfc82 | modified `mwan/go/internal/opnsense/client.go` |
| mwan-mwn1-v2-dispatcher | 34dfc82 | modified `mwan/go/cmd/mwan-opnsense/serve_subcommand.go` |
| mwan-mwn1-v2-transport | 34dfc82 | modified `mwan/go/internal/mwn1/conn.go` |
| mwan-opnsense-config-surgery | 4f8159f | untracked redact script |
| mwan-opnsense-probe-ergonomics | 48dc1d4 | modified `mwan/go/cmd/mwan/opnsense_probe.go` |
| mwan-opnsense-prod-import-transform | d91ce1b | untracked transform script |
| mwan-redact-opnsense-config | 4f8159f | untracked `redact_opnsense_config.py` |
| mwan-tofu-suburban-infra | 76c568b | untracked `opentofu/validate.txt` |
| notify-slice-f-config-parity | b193c7f | untracked `verify-prod.redacted.txt` |
| vm102-apply | 6497f3d | untracked `opentofu/apply-vm102.txt` |
| vm102-tofu-reconcile | 1dcaff5 | untracked `opentofu/plan-after-reconcile.txt` |

## Surprises

`vm102-apikey-and-import` had moved from MERGED with a dirty working tree
(audit captured tip `e13ead6`) to NOT_MERGED with one new unique commit
`a6469c9 Generate VM 102 root API key and add revertBackup runbook` plus an
untracked `import-response.json`, sometime between the initial audit and the
removal pass. This suggests it is actively being used. It was not removed.
It is documented in the "flagged for review" list above so the user is aware
of the tip change.

After the cleanup pass completed, a fresh worktree `vm102-pre-reboot-inspect`
appeared at `78045a8` and the `upgrade-rehearsal-runbook` worktree was
recreated with a new tip `5a9f3da` (it had been removed cleanly during the
pass at tip `78045a8`). Both look like a separate session creating new
worktrees concurrently with this cleanup. They were left alone. The "after
cleanup" total of 42 reflects state at the time of the cleanup pass, before
those concurrent creations.

## Method

```
git fetch origin
git merge-base --is-ancestor <branch-tip> main && echo MERGED || echo NOT_MERGED
git rev-list --count <branch-tip> ^main   # extra check, must be 0 to remove
git worktree remove <wt>
git branch -D <branch>
```

No `push`, no `merge`, no `force` operations were performed. Nothing was
pushed to a remote.
