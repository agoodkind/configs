# configs repo, Claude Code rules

This is the infrastructure configuration repo for goodkind.io homelab.
Read `AGENTS.md` for rules and general guidance.
Read `docs/INFRA.md` for current topology and layout state.
Read `.cursor/commands/deploy-playbook.md` for deploy rules.
Read `.cursor/rules/ssh.mdc` for SSH access patterns.

## Running Ansible playbooks

**The only command pattern that works in this environment:**

```bash
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/<name>.yml"
```

`cd X && ansible-playbook` does not work in this tool context because the Bash tool does not persist `cd` across `&&`-chained commands. The `bash -c "..."` wrapper spawns a subshell where the directory change takes effect, picks up `ansible.cfg`, and resolves the Proxmox dynamic inventory.

If you only need vault variable names, do not run `ansible-vault view`. Run `python3 /Users/agoodkind/Sites/configs/scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass /Users/agoodkind/Sites/configs/ansible/inventory/group_vars/all/vault.yml` instead.

### Examples

```bash
# Deploy or update Traefik and cloudflared on proxy CT, skipping SSHPiper re-deploy.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxy.yml --skip-tags sshpiper"

# Dry-run first.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxy.yml --skip-tags sshpiper --check --diff"

# Deploy tack CT 117.
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-tack.yml"
```

## Surgical Change Protocol

For any production change to proxy, mwan, vault, or OPNsense:

1. SSH to the host and read the live config before trusting repo templates.
2. Make the smallest possible surgical change on the live host.
3. Verify the live change with a specific command.
4. Run the Ansible playbook only after the live change works.

**Never run `ansible-playbook` without first verifying the change surgically on the live host.**

## SSH access

- `ssh proxy`: proxy CT 110, sshd on port 2222, routed via `~/.ssh/config`.
- `ssh tack`: tack CT 117, `3d06:bad:b01::117`.
- `ssh vault`: Proxmox host, `3d06:bad:b01::254`.
- Full patterns live in `.cursor/rules/ssh.mdc`.
