# MWAN Go standards

Standards for Go code under [mwan/go/](../../mwan/go/). Violations block merge.
See [docs/mwan/overview.md](overview.md) for the runtime architecture and
[docs/mwan/script.md](script.md) for shell and OPNsense conventions.

## Monolith contract

All Go infrastructure code lives in one binary built from
[mwan/go/cmd/mwan/](../../mwan/go/cmd/mwan/). The linux/amd64 build is `mwan` on
targets. The freebsd/amd64 build is `mwan-opnsense` and runs only on OPNsense,
where it auto-dispatches into the opnsense daemon based on `argv[0]`.

New tools become subcommands of this binary, never separate binaries. The
authoritative subcommand set is the dispatch in
[mwan/go/cmd/mwan/main.go](../../mwan/go/cmd/mwan/main.go); run `mwan` with no
arguments for current usage. Subcommands are of two kinds: long-running daemons
(the agent, the watchdog, the interface manager, and the opnsense config daemon)
and one-shot operator tools (health probes, delegated-prefix and firewall-state
inspection, and an alert self-test). The interface manager's behavior comes from
`[ifmgr].role` in `/etc/mwan/config.toml`.

Each subcommand composes the shared packages under `mwan/go/internal/` (config
loader, email sender, logger factory, ops layer, BGP speaker, alerting, tracing,
rollback state) rather than reimplementing them.

## Code standards

- **Single TOML config.** All subcommands read `/etc/mwan/config.toml`. No
  env-var-based config loading. Env vars override secrets only
  (`SMTP2GO_API_KEY`, `PVE_TOKEN_SECRET`).
- **No globals.** Config is passed explicitly through function arguments. No
  package-level `var` for config, state, or singletons.
- **DRY.** No duplicated structs, no bridge or adapter types that mirror another
  struct field-by-field. If two things need the same data, they share one type.
- **Small files.** No file over 500 lines. If a file exceeds this, split by
  responsibility.
- **Separated concerns.** Config loading, business logic, I/O, and CLI parsing
  live in separate files. No function that parses flags and runs business logic
  at the same time.
- **One email sender.** One `EmailSender` type, parameterized at construction.
  No per-subcommand email implementations.
- **One logger factory.** One `newLogger()` function parameterized by
  subcommand name, log paths, and an optional email handler. No per-subcommand
  logger setup files.
- **No hardcoded values.** IPs, paths, timeouts, email addresses, and hostnames
  come from TOML config. Validation errors loudly when a required field is
  missing.
- **Comments explain why, not what.** Do not add comments that restate the
  code. Do not add `// Foo does X` when the function name already says X.
- **Secrets in Ansible Vault.** TOML templates use `{{ vault_* }}` Jinja2
  variables. Never commit plaintext secrets. The `.j2` suffix signals a
  template. Full vault contract in [docs/ansible/secrets.md](../ansible/secrets.md).
- **Linting enforced.** `make lint` (golangci-lint) must pass. Config in
  [mwan/go/.golangci.yml](../../mwan/go/.golangci.yml).
## Build rules

Every implementation agent or person making changes must:

- **Start from evidence.** Read the relevant source before changing code.
- **Respect the boundary.** Generic layers stay generic. Provider-specific or
  platform-specific behaviour lives behind the provider boundary. Preserve
  exact user-visible values unless an external boundary requires escaping or
  translation.
- **Implement real behaviour.** Wire features into the real runtime path, not
  only into tests or fallback code. Prefer one source of truth over
  compatibility crutches. Reconcile related state immediately when the
  user-facing contract says values should stay in sync.
- **No shortcuts.** No baseline edits to hide lint findings. No `//nolint`
  without explicit operator authorisation. No synthetic references, dummy
  logs, or marker-method calls to satisfy reachability tools. No no-op closers
  or empty lifecycle methods. No compile-only or log-only tests presented as
  behavioural coverage.
- **Tight types.** Avoid `any`, `interface{}`, and loose maps unless required
  at a real external boundary. Convert untyped input to concrete types as
  early as possible.
- **Useful tests.** Test the real contract. Add regression coverage for the
  failure mode that motivated the change. Avoid tests that only prove
  compilation, only log output, or assert implementation trivia.
- **Verify before reporting.** Run the project's real gates: `make check`,
  `make test`, `make build-linux`, `make build-mwan-opnsense`. State exactly
  what was run and whether it passed. If a gate could not be run, state why.
- **Report honestly.** State what changed, the verification commands, and any
  residual risks. Do not claim files, symbols, commits, or behaviour that
  was not verified.
