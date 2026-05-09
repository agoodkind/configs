# tack MCP server: duplicate-workspace bug (2026-05-08)

## Symptom

`tack_list_workspaces` returns two entries with identical slug `main`:

```
- main (Name: Main, Type: workspace)
- main (Name: Main, Type: workspace)
```

After this duplicate state appeared:
- `tack_create_comment` fails with `Problem: workspace_reference is required`.
- `tack_create_issue` fails with the same error.
- `tack_get_issue` fails with `reference "MWAN-NNN": not found. Check tack_list_workspaces for workspace_reference`.

## Reproduction

Encountered ~21:00 PDT on 2026-05-08 mid-session. Earlier in the same session,
the same workspace_slug=main worked end-to-end (created MWAN-160 through
MWAN-179, posted dozens of comments). The break appeared without an explicit
trigger; the most recent successful operation was tack_create_issue for
MWAN-179 at ~21:30 PDT.

## Workaround

None known. The MCP server schema only exposes `workspace_slug`, but the error
suggests the server now requires a `workspace_reference` parameter that the
schema does not document. Tools that can no longer post comments instead record
their findings in the per-ticket markdown docs under `mwan/docs/`.

## Filed where

This bug cannot be filed as a tack issue (the bug prevents creation). Documented
here in the repo. File via tack UI when reachable.
