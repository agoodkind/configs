# configs repo, Claude Code rules

This is the infrastructure configuration repo for the `goodkind.io` homelab.
Read [AGENTS.md](AGENTS.md) for repo rules and general guidance.
Read [docs/infra/overview.md](docs/infra/overview.md) for current topology and
layout state.
Read [.agents/skills/deploy-playbook/SKILL.md](.agents/skills/deploy-playbook/SKILL.md)
for deploy rules.
Read [docs/infra/access.md](docs/infra/access.md) for SSH access patterns.

## Running Ansible playbooks

The canonical deploy invocation and vault-key listing live in
[AGENTS.md](AGENTS.md). Direct invocation of `ansible`, `ansible-vault`,
`ansible-playbook`, `ansible-inventory`, and `ansible-console` from agent shells
is blocked by agent-gate. The five-playbook table and per-service examples live
in [.agents/skills/deploy-playbook/SKILL.md](.agents/skills/deploy-playbook/SKILL.md).

## Surgical change protocol

For any production change to proxy, MWAN, vault, or OPNsense:

1. SSH to the host and read the live config before trusting repo templates.
2. Make the smallest possible surgical change on the live host.
3. Verify the live change with a specific command.
4. Run the Ansible playbook only after the live change works.

Never run `ansible-playbook` against production first and hope the template is
right.
