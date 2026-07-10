# Wedge-proof drainer (plan: docs/superpowers/plans/2026-06-27-wedgeproof.md)
Branch: 06-24-opnsense-chardev-drainer (PR #82)

Task 1: complete (commit 086e65e, spec OK + quality approved). Decouple reader/writer, drop-client-on-overflow, flush-on-swap. Minor (for final review): chardevWriteLoop c.Write sits outside the ctx select, so the writer can outlive drainChardev by one slow write; not a regression, not a leak.
Task 2: complete (commit 34fa200, spec PASS + quality approved). makeChardevOpener holds reclaimExpected; ERROR on unexpected fresh dial; test added. Minor (final review): reclaimExpected set even on store failure (arguably safer; wants a one-line comment).
Task 3: complete (commit d5714d8, verified directly: one-line RestartSec=2s->0; FileDescriptorStorePreserve=yes + StoreMax=4 + Restart=always already present). All 3 tasks done.
Task 1-3 done; final whole-branch review MERGE-READY (race-tested), no Critical. Important=wedge.md dead link self-resolves via main (#81). Minors logged.
Task 1-3 + 6 copilot fixes merged as PR #82 (53dbc67).
