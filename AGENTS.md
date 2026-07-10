# AGENTS

This repository manages the `goodkind.io` homelab. It contains OpenTofu for
provisioning, Ansible for configuration, MWAN runtime code, and the operator
runbooks that explain how those pieces fit together.

## Documentation

Each subsystem has one overview that is the entry point for that area. Start
there and follow its links, rather than hunting individual files.

- Infrastructure state and inventory: [docs/infra/overview.md](docs/infra/overview.md).
- Ansible inventory, quality, and secret contracts: [docs/ansible/overview.md](docs/ansible/overview.md).
- MWAN runtime, per-host layout, and the suburban testbed: [docs/mwan/overview.md](docs/mwan/overview.md).
- OPNsense steady-state behavior and config import: [docs/opnsense/notes.md](docs/opnsense/notes.md).

## Sources of truth

- Infrastructure state, host notes, and point-in-time topology live under
  [docs/infra/](docs/infra/).
- Canonical service names, hostnames, IPv6 addresses, and service-group entries
  live in
  [ansible/inventory/group_vars/all/service_mapping.yml](ansible/inventory/group_vars/all/service_mapping.yml).
- Static inventory parents and hand-managed inventory groups live in
  [ansible/inventory/hosts](ansible/inventory/hosts).
- Per-hypervisor Proxmox dynamic inventory lives in
  [ansible/inventory/vault.proxmox.yml](ansible/inventory/vault.proxmox.yml)
  and
  [ansible/inventory/suburban.proxmox.yml](ansible/inventory/suburban.proxmox.yml).
- Shared non-secret variables live in
  [ansible/inventory/group_vars/all/vars.yml](ansible/inventory/group_vars/all/vars.yml).
- Shared secrets live in
  [ansible/inventory/group_vars/all/vault.yml](ansible/inventory/group_vars/all/vault.yml).
- Provisioned guest resources live in [opentofu/](opentofu/).

## Deployment contract

The deploy is split cleanly across two repos plus the ops command surface.

1. The configs repo owns Host and LXC. Proxmox and OpenTofu provision the LXC; Ansible brings it to a deployable state (Docker, host networking, the install directory, the audit signing key, the FoundationDB cluster directory, and the rendered `.env`), fetches the app stack from the app repo's GitHub source of truth, and runs `docker compose up -d` to start the full stack. First boot and any full-stack change are Ansible's; it starts the stack but does not build the images, which come from CI.
2. Each app repo owns everything that runs on top of its LXC. For Tack that is `docker-compose.yml` and the overlays (`fdb-overlay/fdb.bash`, `yugabyte-overlay/yugabyted`); they live only in the tack repo and are the single source of truth. Ansible fetches them from GitHub at the deployed ref; the configs-rendered `.env` is the env override.
3. Image build, backups, and app-image deploys live in `./server ops` (Go), not shell, using typed SDKs (the Docker Go SDK, pgx, the FoundationDB bindings). `./server ops deploy` builds, pushes, pulls, runs `docker compose up -d` for `app` and `audit-consumer`, and verifies the running image digest; the full-stack bring-up is Ansible's job (point 1), not this command's. Brittle standalone shell scripts are not used; the legacy `make deploy` path is retired.

## Deployment workflow

OpenTofu provisions every guest. Run `tofu apply` from [opentofu/](opentofu/)
first to create or update the LXC, then run the matching `deploy-<service>.yml`
playbook from [ansible/playbooks/](ansible/playbooks/), which registers the guest
in inventory and configures what runs inside it.

The canonical playbook invocation is:

```bash
go run goodkind.io/configs/cmd/configs deploy <name> [--limit <host>] [--check] [--diff]
```

`rake -C /Users/agoodkind/Sites/configs/ansible deploy:<service>[<limit>]` is
the equivalent shortcut from inside the configs repo. Do not invoke `ansible`,
`ansible-vault`, `ansible-playbook`, `ansible-inventory`, or `ansible-console`
directly.

Use `--limit <host>` for production runs and `--check --diff` before mutating
anything important. Workflow details live in
[.agents/skills/deploy-playbook/SKILL.md](.agents/skills/deploy-playbook/SKILL.md).

## Ansible and secret contract

- [ansible/inventory/group_vars/all/vars.yml](ansible/inventory/group_vars/all/vars.yml)
  stores shared non-secret values only.
- [ansible/inventory/group_vars/all/vault.yml](ansible/inventory/group_vars/all/vault.yml)
  stores all shared secrets, and every secret name starts with `vault_`.
- Files that need a vault-backed secret reference the `vault_*` name directly.
  Do not add pure aliases like `api_key: "{{ vault_api_key }}"`.
- Env-wrapper variables are allowed only when they represent a real environment
  override with a vault fallback and stay local to the service or play that
  needs them.
- Never use `default()` or `is defined` on an input variable. Declare every input
  value explicitly in the service's group_vars, `service_mapping.yml`, or
  OpenTofu, and read it bare; a missing value fails at load time. `default()` and
  `is defined` are allowed only on module or register output (a command result's
  shape). Enforced by `configs lint`, which the deploy command runs before every
  deploy and pre-commit runs on staged files; see `docs/ansible/quality.md`. The
  linter parses each Jinja expression with a Go engine and routes the few
  Ansible-Jinja forms that engine cannot read to a jinja2 reference parser
  (`scripts/lint_ansible_ast.py`), so it requires `python3` with the `jinja2`
  package on PATH.
- The Proxmox inventory plugins read `token_secret` directly from
  [ansible/inventory/group_vars/all/vault.yml](ansible/inventory/group_vars/all/vault.yml).
  Do not move those secrets into shell startup files just to make a playbook work.
- Never pipe decrypted vault contents into anything that could echo them back to
  chat. If you only need key names, run:

```bash
go run goodkind.io/configs/cmd/configs keys
```

## Production change protocol

Production hosts serve live traffic for people who cannot recover from a bad
change on their own. Read the live host before you trust repo templates, state a
hypothesis, test the smallest reversible change, verify no regression, and only
then codify the change in git.

Do not bulk-change MWAN, OPNsense, or the vault hypervisor. Do not restart
networking services without a rollback path. When a runbook says `STOP`, stop,
capture forensics, and reset to a known-good state instead of improvising.

## Repository rules

- IPv6 is P0. Prefer IPv6 literals in configs and test IPv6 reachability first.
  Diagnosis rules live in [docs/infra/network.md](docs/infra/network.md).
- SSH entry points and jump-host patterns live in
  [docs/infra/access.md](docs/infra/access.md).
- Keep current-state facts in [docs/infra/](docs/infra/). Keep subject-specific
  contracts and policies in [docs/ansible/](docs/ansible/),
  [docs/mwan/](docs/mwan/), or [docs/opnsense/](docs/opnsense/).
- Do not create new docs unless the operator asks for them.
- Use `git -C /Users/agoodkind/Sites/configs ...` because shell cwd is not
  reliable across worktrees and subshells.
- Berylax is offline. Treat its notes as historical only.

## Implementation rules for agents

- Start from evidence. Read the code and the relevant local docs before editing.
- Respect boundaries. Keep platform-specific behavior behind the relevant
  boundary instead of leaking it into generic layers.
- Implement the real runtime path. Do not hide the problem with fallback-only
  code, lint suppressions, dummy logs, or compile-only tests.
- Keep types tight and reuse existing source-of-truth structs where possible.
- Add tests that cover the actual regression or contract.
- Preserve unrelated user changes. Do not revert work you did not make.
- Verify with the real project gates when the change warrants it, and state what
  ran and what did not.
- Report what changed, what was verified, and what residual risk remains.
