# configs repo — Claude Code rules

This is the infrastructure configuration repo for goodkind.io homelab.
Read `AGENTS.MD` for full context. Read `.cursor/commands/deploy-playbook.md` for deploy rules.
Read `.cursor/rules/ssh.mdc` for SSH access patterns.

## Running Ansible playbooks

**The only command pattern that works in this environment:**

```bash
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/<name>.yml"
```

`cd X && ansible-playbook` does NOT work — the Bash tool does not persist `cd` across
`&&`-chained commands. The `bash -c "..."` wrapper spawns a subshell where the directory
change takes effect, picks up `ansible.cfg`, and resolves the Proxmox dynamic inventory.

### Examples

```bash
# Deploy/update Traefik + cloudflared on proxy CT (skip SSHPiper re-deploy)
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxy.yml --skip-tags sshpiper"

# Dry-run first
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxy.yml --skip-tags sshpiper --check --diff"

# Deploy tack (CT 117)
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-tack.yml"
```

## Surgical Change Protocol (summary — full version in AGENTS.MD)

For any production change (proxy, mwan, vault, OPNsense):

1. SSH to the host and read the **live** config — do not trust repo templates as ground truth
2. Make the smallest possible surgical change on the live host
3. Verify it works
4. Then run the Ansible playbook to codify it

**Never run `ansible-playbook` without first verifying the change surgically on the live host.**

## SSH access

- `ssh proxy` — proxy CT (CT 110), sshd on port 2222, routed via `~/.ssh/config`
- `ssh tack` — tack CT (CT 117), `3d06:bad:b01::117`
- `ssh vault` — Proxmox host, `3d06:bad:b01::254`
- Full patterns in `.cursor/rules/ssh.mdc`
