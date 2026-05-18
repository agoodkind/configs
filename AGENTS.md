# AGENTS

This is the infrastructure configuration repository for `goodkind.io`. It contains Ansible
playbooks for LXC/VM provisioning, network device configs (Traefik, KEA DHCP, BIND), the
multi-WAN load balancer setup, and operational docs for the homelab.

Current IPs, bridges, services, tunnels, SSH access, network topology, and
Cloudflare config live in [docs/infra/INFRA.md](docs/infra/INFRA.md).

## Sources of Truth

- **Infrastructure state** (IPs, bridges, services, tunnels): [docs/infra/INFRA.md](docs/infra/INFRA.md)
- **Container/VM hostnames and IPv6 addresses**: `ansible/inventory/group_vars/all/service_mapping.yml`
- **Static inventory and host groups** (hypervisors, OPNsense SSH targets):
  `ansible/inventory/hosts`
- **Dynamic Proxmox inventory** (one file per hypervisor):
  `ansible/inventory/vault.proxmox.yml`, `ansible/inventory/suburban.proxmox.yml`
- **Per-target variables**: `ansible/inventory/group_vars/<group>_servers.yml`
- **Shared non-secret variables**: `ansible/inventory/group_vars/all/vars.yml`
- **Secrets** (encrypted): `ansible/inventory/group_vars/all/vault.yml`

## Deployment Workflow

**New containers are provisioned by OpenTofu** (see `opentofu/`). Run `tofu apply` from
that directory first, then run the corresponding Ansible playbook to configure the
container. Existing containers (pre-OpenTofu) are still created by Ansible's
`create-ct.yml` until they are migrated.

OpenTofu state is stored in Consul at `opentofu/state`. Credentials go in
`opentofu/terraform.tfvars` (gitignored; see `terraform.tfvars.example`).

Five canonical playbooks own the MWAN/Proxmox/OPNsense surface; each one targets
a specific group so prod-vs-testbed differences come from group_vars, not from
playbook conditionals:

- `playbooks/deploy-proxmox.yml`: Proxmox hypervisor work (mwan-ifmgr,
  mwan-watchdog, cloudflared-oob). Target `proxmox_servers`.
- `playbooks/deploy-mwan.yml`: MWAN VM (prod VM 113 on vault). Target
  `mwan_servers`.
- `playbooks/deploy-mwan-failover.yml`: MWAN failover LXC. Target
  `mwan_failover_servers` (prod) or `mwan_failover_test_servers` (testbed).
- `playbooks/deploy-opnsense.yml`: mwan-opnsense daemon. Target
  `opnsense_servers` (prod) or `opnsense_test_servers` (testbed).
- `playbooks/deploy-testbed.yml`: suburban-only extras (`qm args`, ISP LXCs,
  host bridge, VFIO). Target `suburban_servers`.

Other `deploy-<service>.yml` playbooks (proxy, adguard, ddns, tack, etc.) follow
the same convention but target a single service. Always use `--limit <host>` for
production runs and `--check --diff` for dry runs first. The local `ansible/Rakefile`
wraps the canonical five with shortcuts (`rake deploy:proxmox[vault]`,
`rake check:mwan`, `rake syntax:all`).

See [.cursor/commands/deploy-playbook.md](.cursor/commands/deploy-playbook.md)
for the full invocation pattern. Ansible setup and inventory contracts live in
[docs/ansible/ANSIBLE.md](docs/ansible/ANSIBLE.md). Vault rules and the safe
key-listing command live in [docs/ansible/SECRETS.md](docs/ansible/SECRETS.md).

## Surgical Change Protocol

Production hosts (vault, mwan, OPNsense) serve live traffic for non-technical
users who cannot recover from outages. Berylax is indefinitely offline for now;
historical host and OOB notes live in [docs/BERYLAX.md](docs/BERYLAX.md). Physical
access to hardware is unavailable for months at a time. Treat every change as
potentially irreversible.

**Before any change to a production host:**

1. **Understand the current state.** SSH in and read live config, routes, rules, logs.
   Do not trust [docs/infra/INFRA.md](docs/infra/INFRA.md) or Ansible templates as ground truth; they drift.
2. **Form a testable hypothesis.** State what you expect the change to do and what would
   prove it worked.
3. **Test surgically.** Apply the smallest possible change, verify with a specific command,
   then remove it. Example: add one ip6 rule, verify route lookup changed, run one ping,
   remove the rule.
4. **Verify no regression.** After confirming the fix, check that forwarded traffic, load
   balancing, and other paths still work before making anything permanent.
5. **Then codify.** Only after the live test passes, write the change into the Ansible
   template or script in the repo.
6. **Never bulk-change production.** No `ansible-playbook` runs against mwan without
   verifying each component independently first. No `systemctl restart` of networking
   services without a rollback plan.

## Operational Pointers

- Current vault hypervisor state lives in [docs/infra/vault.md](docs/infra/vault.md).
- Current MWAN host topology, live unit names, repository layout state, manual rollout order,
  stale binary inventory, and cleanup notes live in [docs/infra/mwan-layout.md](docs/infra/mwan-layout.md).
- Current suburban testbed state lives in [docs/infra/suburban-testbed.md](docs/infra/suburban-testbed.md).
- Current non-vault host state lives in [docs/infra/hosts.md](docs/infra/hosts.md).
- Current OPNsense network topology lives in [docs/infra/opnsense.md](docs/infra/opnsense.md).
- Current Cloudflare account, tunnel, WARP route, load balancer, and DNS state lives in
  [docs/infra/cloudflare.md](docs/infra/cloudflare.md).
- MWAN design, BGP failover, graceful restart, watchdog behavior, and snapshot rules live in
  [docs/MWAN.md](docs/MWAN.md).
- OPNsense BGP steady state, operational foot-guns, and recovery snippets live in
  [docs/runbooks/OPNSENSE-OPERATIONAL-NOTES.md](docs/runbooks/OPNSENSE-OPERATIONAL-NOTES.md).
- OPNsense config-import internals live in
  [docs/runbooks/opnsense-25.7-config-import-flow.md](docs/runbooks/opnsense-25.7-config-import-flow.md).
- The OPNsense testbed config-import gate lives in
  [docs/runbooks/opnsense-testbed-config-import.md](docs/runbooks/opnsense-testbed-config-import.md).
- MWAN email and alert routing lives in [docs/plans/mwan-email-routing.plan.md](docs/plans/mwan-email-routing.plan.md).
- Berylax is indefinitely offline for now. Historical berylax host, Cloudflare,
  and OOB serial notes live in [docs/BERYLAX.md](docs/BERYLAX.md).
- Emergency out-of-band access state lives in [docs/infra/oob.md](docs/infra/oob.md).

## Monolith Contract

All Go infrastructure code lives in one binary built from `mwan/go/cmd/mwan/`. The
linux/amd64 build is `mwan`; the freebsd/amd64 build is `mwan-opnsense` and runs only
on OPNsense, where it auto-dispatches into the `opnsense` daemon based on its argv[0].

There are NO separate Go binaries. New tools become subcommands of this monolith.
Shared code lives under `internal/config`, `internal/email`, `internal/logging`,
`internal/ops`, `internal/bgp`, `internal/alert`, `internal/tracing`, `internal/mwn1`,
`internal/rollback`.

Current MWAN command surfaces and host layout live in [docs/infra/mwan-layout.md](docs/infra/mwan-layout.md).
MWAN failover and watchdog behavior live in [docs/MWAN.md](docs/MWAN.md).

## Build rules for implementation agents

Every implementation agent, whether dispatched as a subagent or running inline, must apply these:

- **Start from evidence.** Read the relevant source before changing code. Read this file and any local design doc the change touches. Do not assume architecture from names alone.
- **Respect the boundary.** Generic layers stay generic. Provider-specific or platform-specific behavior lives behind the provider boundary. Preserve exact user-visible values unless an external boundary requires escaping or translation.
- **Implement the real behavior.** Wire features into the real runtime path, not only into tests or fallback code. Prefer one source of truth over compatibility crutches. Reconcile related state immediately when the user-facing contract says values should stay in sync. Avoid deferred cleanup.
- **Avoid shortcuts.** No baseline edits to hide lint findings. No `//nolint` without explicit operator authorization. No synthetic references, dummy logs, or marker-method calls to satisfy reachability tools. No no-op closers or empty lifecycle methods. No compile-only or log-only tests presented as behavioral coverage.
- **Keep types tight.** Avoid `any`, `interface{}`, and loose maps unless required at a real external boundary. Convert untyped input to concrete types as early as possible.
- **Write useful tests.** Test the real contract. Add regression coverage for the failure mode that motivated the change. Avoid tests that only prove compilation, only log output, or assert implementation trivia.
- **Preserve project hygiene.** Keep edits inside scope. Do not revert unrelated work. Update comments and docs when they would otherwise describe the old contract.
- **Verify before reporting.** Run the project's real gates: `make check`, `make test`, `make build-linux`, `make build-mwan-opnsense`. State exactly what was run and whether it passed. If a gate could not be run, state why.
- **Report honestly.** State what changed. State the verification commands. State residual risks. Do not claim files, symbols, commits, or behavior that was not verified. Every factual claim must trace to a command run in this session with the output cited verbatim. No "likely", "probably", or "should" without a verifying command.

## LLM Writing Guidelines

Treat every statement here as guidance for how to write and ingest material in this repo, not
as asserted fact about any specific host. If anything conflicts with a primary source (a file
in git, a man page, or output you reproduced), prefer the primary source and treat this
document as stale until someone updates it.

**Default stance:** Assume claims are uncertain until tied to evidence. Prefer "it appears" when
the basis is a single file or log snippet. Prefer "this suggests" when inferring from several
weak signals. State "no verifiable source is available" explicitly rather than filling gaps
with confidence.

**Evidence discipline:** For each non-trivial statement, tie it to a repo path, a command with
representative output, or an external URL. When treating something as proof, name the evidence
first, then give the conclusion qualified by that evidence.

**Investigatory tone:** Write as if the reader is joining an ongoing investigation. Prefer "it
may be worth checking" over "you must". Offer options and describe what to observe if someone
tries them.

**Conflicts between sources:** List disagreements without forcing resolution. Note which source
is usually authoritative for that layer only if a cited policy or comment in the repo supports
it. Suggest a single reproducible check that would break the tie if one exists.

**Staleness:** Every statement should carry implicit scope (environment, date or git ref if
known, and what would make the note obsolete). Infra drifts; "last verified" beats
"timeless truth".

**Secrets:** Never copy tokens, passwords, private keys, or session cookies. Refer to vault keys,
env var names, or rotation procedures. If a secret's location must be described, use path plus
permission model, not content.

**Shell and code notes:** For shell, note bash vs zsh when expansion or builtins differ. For
Ansible, follow the quality rules in [.cursor/rules/ansible-quality.mdc](.cursor/rules/ansible-quality.mdc). For mwan scripts,
follow the shell style rules in [.cursor/rules/mwan.mdc](.cursor/rules/mwan.mdc).

## Prose rule

Prose reads cleanly as a linear record of the thing itself. Each sentence is a full sentence with a concrete subject, a concrete verb, and enough context to sound natural when spoken aloud. Each new sentence adds useful information in the same direction as the sentence before it, with low cognitive load and no hidden context the reader must reconstruct. Paragraphs move forward by accumulation, with no setup, interruption, reversal, or correction.

## Go Code Standards

These rules apply to all Go code in `mwan/go/`. Violations block merge.

- **Single TOML config.** All subcommands read `/etc/mwan/config.toml`. No env-var-based
  config loading. Env vars override secrets only (`SMTP2GO_API_KEY`, `PVE_TOKEN_SECRET`).
- **No globals.** Config is passed explicitly through function arguments. No package-level
  `var` for config, state, or singletons.
- **DRY.** No duplicated structs, no bridge/adapter types that mirror another struct
  field-by-field. If two things need the same data, they share one type.
- **Small files.** No file over 500 lines. If a file exceeds this, split by responsibility.
- **Separated concerns.** Config loading, business logic, I/O, and CLI parsing live in
  separate files. No function that parses flags AND runs business logic.
- **One email sender.** One `EmailSender` type, parameterized at construction. No
  per-subcommand email implementations.
- **One logger factory.** One `newLogger()` function parameterized by subcommand name, log
  paths, and optional email handler. No per-subcommand logger setup files.
- **No hardcoded values.** IPs, paths, timeouts, email addresses, hostnames come from TOML
  config. Validation errors loudly if a required field is missing.
- **Comments explain WHY, not WHAT.** Do not add comments that restate the code. Do not add
  `// Foo does X` when the function name already says X.
- **Secrets in Ansible Vault.** Templates that need secrets reference `vault_*`
  values directly. Never commit plaintext secrets. The `.j2` suffix signals a
  template.
- **Linting enforced.** `make lint` (golangci-lint) must pass. Config in `mwan/go/.golangci.yml`.
- **Cutover is complete.** The `mwan cutover` and `mwan cutover2` subcommands have
  been removed from the binary. Ongoing failover is handled by `mwan watchdog failover`.

## Rules for Changes

1. Before editing any playbook or template, check the Ansible quality rules in
   [.cursor/rules/ansible-quality.mdc](.cursor/rules/ansible-quality.mdc). It documents common pitfalls around single-bracket
   tests, `set_fact` concurrency, folded block scalars in URLs, and guard clause patterns.
2. Shell scripts in `mwan/scripts/` must use `[[ ]]` for tests, full `if/then/fi` blocks
   with no inline ternaries, and pass `shellcheck --severity=error`. The full style
   requirements are in [.cursor/rules/mwan.mdc](.cursor/rules/mwan.mdc).
3. Secret values go in `ansible/inventory/group_vars/all/vault.yml` (Ansible Vault
   encrypted) under `vault_*` names. Shared non-secret defaults go in
   `ansible/inventory/group_vars/all/vars.yml`. Do not add pure aliases like
   `foo: "{{ vault_foo }}"` to shared or service `group_vars` files. If a
   consumer needs env fallback, keep that wrapper local to the service or play. For
   new services provisioned via OpenTofu, per-service generated secrets (db
   passwords, secret keys) may use Ansible's `lookup('password', ...)` plugin,
   which caches values in `<service>/.secrets/` (gitignored) on the Ansible
   controller.
4. IPv6 is P0. The diagnosis workflow is in [.cursor/rules/ipv6-dhcp-diagnosis.mdc](.cursor/rules/ipv6-dhcp-diagnosis.mdc).
5. The `kea/` Rakefile is the live mechanism for pushing DHCP config to the router.
   Do not modify KEA config files without understanding the Rake deploy step first.
6. Always invoke git with `-C /path/to/repo` because shell cwd is unreliable across
   worktrees and subshells. A bare `git push` or `git commit` can land in the wrong
   repo. The agent-gate hook blocks raw `git` invocations.
7. `ansible-vault view` output is sensitive. Do not run it in a way that can reach the
   conversation log, and do not pipe it through `grep`, `awk`, or similar filters. If you
   only need variable names, use `python3 scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass ansible/inventory/group_vars/all/vault.yml`.
8. Capture forensics when the runbook says STOP. Do not improvise, do not retry, and
   surface the state to the operator.
