# configs repo, Claude Code rules

This is the infrastructure configuration repo for the `goodkind.io` homelab.
Read [AGENTS.md](AGENTS.md) for repo rules and general guidance.
Read [docs/infra/overview.md](docs/infra/overview.md) for current topology and
layout state.
Read [.agents/skills/deploy-playbook/SKILL.md](.agents/skills/deploy-playbook/SKILL.md)
for deploy rules.
Read [docs/infra/access.md](docs/infra/access.md) for SSH access patterns.

## Running Ansible playbooks

The canonical command pattern in this environment is:

```bash
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/<name>.yml"
```

The `bash -c` wrapper is required here so the `cd` takes effect in the same
subshell that runs `ansible-playbook`, picks up `ansible.cfg`, and resolves the
dynamic inventory.

If you only need vault variable names, do not run `ansible-vault view`. Run:

```bash
python3 /Users/agoodkind/Sites/configs/scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass /Users/agoodkind/Sites/configs/ansible/inventory/group_vars/all/vault.yml
```

### Examples

```bash
# Configure both Proxmox hypervisors.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxmox.yml"

# Configure only the vault hypervisor.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxmox.yml --limit vault"

# Configure the production MWAN VM.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-mwan.yml"

# Dry-run the production MWAN VM.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-mwan.yml --check --diff"

# Configure the testbed MWAN failover LXC.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-mwan-failover.yml --limit mwan_failover_test_servers"

# Configure the production OPNsense daemon.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-opnsense.yml --limit opnsense_servers"

# Apply suburban-only testbed extras.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-testbed.yml --limit suburban"

# Deploy or update Traefik and cloudflared on proxy CT.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxy.yml --skip-tags sshpiper"

# Deploy tack.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-tack.yml"
```

## Surgical change protocol

For any production change to proxy, MWAN, vault, or OPNsense:

1. SSH to the host and read the live config before trusting repo templates.
2. Make the smallest possible surgical change on the live host.
3. Verify the live change with a specific command.
4. Run the Ansible playbook only after the live change works.

Never run `ansible-playbook` against production first and hope the template is
right.
