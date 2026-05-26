# AGENTS

This repository manages the `goodkind.io` homelab. It contains OpenTofu for
provisioning, Ansible for configuration, MWAN runtime code, and the operator
runbooks that explain how those pieces fit together.

## Topics index

- Current infrastructure state: [docs/infra/overview.md](docs/infra/overview.md)
- SSH access and network diagnosis: [docs/infra/access.md](docs/infra/access.md),
  [docs/infra/network.md](docs/infra/network.md)
- Ansible inventory, quality rules, secrets, and Proxmox API setup:
  [docs/ansible/overview.md](docs/ansible/overview.md),
  [docs/ansible/quality.md](docs/ansible/quality.md),
  [docs/ansible/secrets.md](docs/ansible/secrets.md),
  [docs/ansible/proxmox-api.md](docs/ansible/proxmox-api.md)
- MWAN architecture and coding rules: [docs/mwan/overview.md](docs/mwan/overview.md),
  [docs/mwan/go-standards.md](docs/mwan/go-standards.md),
  [docs/mwan/script-style.md](docs/mwan/script-style.md)
- OPNsense steady-state behavior and import runbooks:
  [docs/opnsense/operational-notes.md](docs/opnsense/operational-notes.md),
  [docs/opnsense/config-import.md](docs/opnsense/config-import.md),
  [docs/opnsense/testbed-baseline.md](docs/opnsense/testbed-baseline.md),
  [docs/opnsense/testbed-config-import.md](docs/opnsense/testbed-config-import.md)
- Historical and forward-looking notes: [docs/infra/berylax.md](docs/infra/berylax.md),
  [docs/infra/wireguard-roaming.md](docs/infra/wireguard-roaming.md),
  [docs/plans/mwan-email-routing.plan.md](docs/plans/mwan-email-routing.plan.md)

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

## Deployment workflow

OpenTofu is the forward path for provisioning. Run `tofu apply` from
[opentofu/](opentofu/) first, then run the matching `deploy-<service>.yml`
playbook from [ansible/playbooks/](ansible/playbooks/).

Legacy guest-creation playbooks such as
[ansible/playbooks/create-ct.yml](ansible/playbooks/create-ct.yml) still exist
for older hosts. Treat them as migration-era exceptions, not as the default
provisioning path.

The canonical playbook invocation in this environment is:

```bash
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py deploy <name> [--limit <host>] [--check] [--diff]
```

The helper spawns `ansible-playbook` as a subprocess from
[ansible/](ansible/) so the inner call never goes through an agent Bash tool
dispatch. `rake -C /Users/agoodkind/Sites/configs/ansible deploy:<service>[<limit>]`
is the equivalent shortcut from inside the configs repo. Direct invocation of
`ansible`, `ansible-vault`, `ansible-playbook`, `ansible-inventory`, and
`ansible-console` from agent shells is blocked by agent-gate because every one
of those decrypts vault values into stdout.

Use `--limit <host>` for production runs and `--check --diff` before mutating
anything important. Shortcuts live in [ansible/Rakefile](ansible/Rakefile), and
workflow details live in [.agents/skills/deploy-playbook/SKILL.md](.agents/skills/deploy-playbook/SKILL.md).

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
- Do not use `default()` or `is defined` to mask a variable that is actually
  required. If the value must exist, fail loudly. Use defaults only when the
  normal case is that the value may be omitted.
- The Proxmox inventory plugins read `token_secret` directly from
  [ansible/inventory/group_vars/all/vault.yml](ansible/inventory/group_vars/all/vault.yml).
  Do not move those secrets into shell startup files just to make a playbook work.
- Never pipe decrypted vault contents into anything that could echo them back to
  chat. If you only need key names, run:

```bash
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py keys
```

## Production change protocol

Production hosts serve live traffic for people who cannot recover from a bad
change on their own. Read the live host before you trust repo templates, state a
hypothesis, test the smallest reversible change, verify no regression, and only
then codify the change in git.

Do not bulk-change MWAN, OPNsense, or the vault hypervisor. Do not restart
networking services without a rollback path. When a runbook says `STOP`, stop,
capture forensics, and reset to a known-good state instead of improvising.

## Operational pointers

- Vault hypervisor state: [docs/infra/vault.md](docs/infra/vault.md)
- MWAN host layout:
  [docs/infra/mwan-layout.md](docs/infra/mwan-layout.md)
- Suburban testbed bridges and guests:
  [docs/infra/suburban-testbed.md](docs/infra/suburban-testbed.md)
- Production OPNsense topology: [docs/infra/opnsense.md](docs/infra/opnsense.md)
- Non-vault hosts and historical berylax state:
  [docs/infra/hosts.md](docs/infra/hosts.md)
- Cloudflare tunnels, WARP routes, and DNS state:
  [docs/infra/cloudflare.md](docs/infra/cloudflare.md)
- Emergency out-of-band status: [docs/infra/oob.md](docs/infra/oob.md)
- OPNsense steady-state foot-guns:
  [docs/opnsense/operational-notes.md](docs/opnsense/operational-notes.md)
- OPNsense import internals:
  [docs/opnsense/config-import.md](docs/opnsense/config-import.md)
- OPNsense testbed baseline and import gate:
  [docs/opnsense/testbed-baseline.md](docs/opnsense/testbed-baseline.md),
  [docs/opnsense/testbed-config-import.md](docs/opnsense/testbed-config-import.md)
- MWAN runtime design and rollout behavior:
  [docs/mwan/overview.md](docs/mwan/overview.md)
- MWAN email routing target state:
  [docs/plans/mwan-email-routing.plan.md](docs/plans/mwan-email-routing.plan.md)

Treat the MWAN email-routing plan as forward-looking until a live check proves
that a specific slice is deployed.

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
