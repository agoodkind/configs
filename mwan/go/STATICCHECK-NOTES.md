# clyde-staticcheck notes for mwan/go

`make staticcheck-clyde` runs Clyde's custom analyzer set on this module.
Clyde's checks are AST-based and intentionally conservative; some of its
rules trigger in places that are deliberate choices for this codebase.
This file documents the exceptions so the next person doesn't try to
"fix" them.

## hot_loop_info_log (~27 instances; intentional)

Clyde's rule: do not emit Info-level slog events inside any `for`/`range`
loop. The intent is to discourage per-row tracing in hot data paths.

In mwan, the matched call sites are all **low-frequency orchestration
loops** that need to be human-observable in real time:

| Site | Loop bound | Why Info |
|---|---|---|
| `cutover/main.go:104,119`, `cutover/preflight.go:39,44`, `cutover/verify.go:23,28` | <10 phases per cutover | "PHASE START / PHASE OK" is the operator-facing milestone |
| `opnsense/client.go` (×7) | <10 neighbors / route-maps / firewall rules | Per-API-object log lets us correlate to OPNsense GUI |
| `cutover2/main.go` (×6), `cutover2/unfuck.go:63`, `cutover2/autorollback.go:94` | <10 gateways/peers/phases | Live output during a 70s cutover; Debug would be too noisy to follow |
| `ops/channels.go:92` | per-channel summary | Already a summary-style emit |
| `healthcheck/main.go:49` | per-target | Operator wants to see each target tick |
| `watchdog/watchdog.go:320,328,346,745,1300,1324` | watchdog main loop and per-iface walks | Boundary signal |

If you find yourself adding a loop that runs more than ~50 times per
event, either downgrade those Info calls to Debug or aggregate to a
summary, but do **not** silently flip every Info to Debug to make this
analyzer happy.

## no_any_or_empty_interface (1 instance; intentional)

`internal/config/config.go:238`:
```go
Modules map[string]map[string]any `toml:"modules"`
```

`ifmgr` modules are a plugin-shaped registry. Each module owns its own
config schema and decodes from the raw `map[string]any` in its
`Constructor`. This is exactly the dynamic-boundary pattern Clyde
allowlists for its own codex/openai adapters; we don't run
clyde-staticcheck with an editable allowlist, so this finding stays
visible. **Do not** flatten to a typed schema that would force every
module's keys into the parent struct.

## slog_error_without_err (resolved)

Every error-level slog event now carries an `err` field. State events
that are not error conditions have been demoted to Warn.

## banned_direct_output (resolved)

`internal/watchdog/main.go` `--list-scenarios` writes are explicit
`fmt.Fprintln(os.Stdout, ...)` calls; user-facing CLI output, not
production diagnostics.

## missing_boundary_log (resolved)

`cmd/mwan/main.go` `main()` emits a `slog.Info("mwan boundary", ...)`
with build identity and the chosen subcommand before any subcommand
dispatch.
