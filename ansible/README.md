# Ansible Configuration

Automated deployment and configuration management for goodkind.io infrastructure.

## Quick Start

### CLI Usage

```bash
# Run a playbook
ansible-playbook playbooks/deploy-proxy.yml

# Target specific hosts
ansible-playbook playbooks/deploy-mwan.yml --limit mwan

# Check what would change (dry-run)
ansible-playbook playbooks/create-ct.yml --check --diff
```

Vault password is configured in `ansible.cfg` and stored in `~/.config/ansible/vault.pass`.

### Semaphore Usage

Playbooks run automatically via [Semaphore UI](https://ansible.home.goodkind.io).

Vault password is configured as environment variable in Semaphore.

## Secrets Management

**All secrets are stored in Ansible Vault** (single source of truth).

- **Documentation**: See [VAULT-SETUP.md](VAULT-SETUP.md)
- **Quick Reference**: See [VAULT-QUICKREF.md](VAULT-QUICKREF.md)

## Directory Structure

```
.
├── ansible.cfg                   # Ansible configuration
├── inventory/                    # Inventory and variables
│   ├── hosts                     # Static inventory
│   └── group_vars/
│       └── all/
│           ├── vault.yml         # Encrypted secrets (Ansible Vault)
│           └── vars.yml          # Non-secret variables
├── playbooks/                    # Playbooks
│   ├── create-ct.yml             # Create LXC containers
│   ├── deploy-mwan.yml           # Deploy multi-WAN setup
│   ├── deploy-proxy.yml          # Deploy reverse proxy
│   └── tasks/                    # Reusable task files
└── roles/                        # Ansible roles (if any)
```

## Common Tasks

### View Secrets
```bash
ansible-vault view inventory/group_vars/all/vault.yml
```

### Edit Secrets
```bash
ansible-vault edit inventory/group_vars/all/vault.yml
```

### Add New Secret
1. Edit vault: `ansible-vault edit inventory/group_vars/all/vault.yml`
2. Add: `vault_new_secret: "value"`
3. Reference in vars: `new_secret: "{{ vault_new_secret }}"`
4. Commit both files

## Setup for New Team Members

1. **Clone repository**:
   ```bash
   git clone <repo-url>
   cd ansible
   ```

2. **Get vault password** from team password manager

3. **Store password locally**:
   ```bash
   mkdir -p ~/.config/ansible
   chmod 700 ~/.config/ansible
   echo 'VAULT_PASSWORD_HERE' > ~/.config/ansible/vault.pass
   chmod 600 ~/.config/ansible/vault.pass
   ```

4. **Install dependencies**:
   ```bash
   ansible-galaxy collection install -r requirements.yml
   ```

5. **Test vault access**:
   ```bash
   ansible-vault view inventory/group_vars/all/vault.yml
   ```

## Documentation

- [Vault Setup Guide](VAULT-SETUP.md) - Complete secrets management documentation
- [Vault Quick Reference](VAULT-QUICKREF.md) - Common commands cheat sheet

## Support

- **Semaphore UI**: https://ansible.home.goodkind.io
- **Documentation**: See docs in this directory
