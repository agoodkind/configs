---
description: Deploy Ansible playbooks from the configs repo
---

# Deploy Ansible Playbook

Run from the `ansible/` directory with explicit vault password:

```bash
cd /Users/agoodkind/Sites/configs/ansible
ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/<playbook>.yml
```

## Examples

```bash
# Deploy MWAN
ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-mwan.yml

# Deploy proxy (Traefik + SSHPiper)
ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxy.yml

# Deploy AdGuard
ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-adguard.yml
```

## Available playbooks

- `deploy-adguard.yml` - AdGuard Home DNS server
- `deploy-dns64.yml` - DNS64 configuration
- `deploy-grommunio.yml` - Grommunio email server
- `deploy-mwan.yml` - Multi-WAN configuration
- `deploy-nanomdm.yml` - NanoMDM device management
- `deploy-proxy.yml` - Traefik reverse proxy + SSHPiper
- `deploy-semaphore.yml` - Semaphore automation server
- `deploy-ssh-keys.yml` - SSH key deployment

## Notes

- Must run from `ansible/` directory (where `ansible.cfg` lives)
- Vault password file: `~/.config/ansible/vault.pass`
