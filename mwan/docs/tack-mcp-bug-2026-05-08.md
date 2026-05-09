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

## Tickets queued for filing once tack MCP recovers

- (would-be MWAN-180): `mwan opnsense-upgrade validate` hangs while standalone `mwan opnsense-validate` works. Both use the same envFactory and `validate.Run`. Standalone returns in ~13s; the upgrade subcommand sticks in `Client.Call` waiting on a BGP neighbor check Exec. `cmd/mwan/opnsense_env_transport.go:133` sets `ExecTimeoutSeconds: 0` on the validate-via-upgrade path so the per-RPC has no deadline. Standalone path apparently propagates a deadline. Workaround: run standalone `mwan opnsense-validate`, then patch `state.json` from `executed` to `validated_pass`. Found during rehearsal 8 (commit 2417ffa, merged as 6ca6eb5).

- (would-be MWAN-181): `mwan opnsense-upgrade execute` returns based on `opnsense-update -u` script exit code, but the actual install happens on the next boot via rc.d consuming `.base.pending` etc. The orchestrator has no reboot trigger and no post-reboot version check. Operators have to issue `shutdown -r +0` manually then probe for the new version. Should be a built-in step in execute or a separate `reboot` subcommand so the state machine records that the reboot happened. Found during rehearsal 8.

- (would-be MWAN-182): MWAN-168 is functionally Done as of commit 6ca6eb5. BGP v4+v6 convergence on VM 102 verified, DNS resolves via Cloudflare forwarders, default routes installed via BGP, full upgrade rehearsal completes end-to-end. Close MWAN-168 once tack MCP recovers.
