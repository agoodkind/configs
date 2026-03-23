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

All secrets are stored in Ansible Vault (single source of truth).

- **Documentation**: See [SECRETS.md](SECRETS.md)

## Directory Structure

```
.
├── ansible.cfg                   # Ansible configuration
├── inventory/                    # Inventory and variables
│   ├── hosts                     # Static inventory
│   ├── proxmox.yml               # Dynamic Proxmox inventory plugin
│   └── group_vars/
│       └── all/
│           ├── vault.yml         # Encrypted secrets (Ansible Vault)
│           ├── vars.yml          # Non-secret variables
│           └── service_mapping.yml  # Single source of truth for host IPs
├── playbooks/                    # Playbooks (one per service)
│   ├── create-ct.yml             # Provision LXC containers
│   ├── prep-guests.yml           # Bootstrap all LXCs (packages, msmtp, Consul, updater)
│   ├── deploy-mwan.yml           # Multi-WAN VM
│   ├── deploy-proxy.yml          # Traefik + SSHPiper
│   ├── deploy-adguard.yml        # AdGuard Home
│   ├── deploy-dns64.yml          # BIND DNS64
│   ├── deploy-consul.yml         # Consul server
│   ├── deploy-consul-external.yml # Consul agents on vault, NAS, mini, OPNsense
│   └── deploy-grommunio.yml      # Grommunio (not wired into any workflow)
└── templates/                    # Jinja2 templates organized by host type
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

- [Secrets Management](SECRETS.md) - Vault setup and common commands
- [Proxmox Setup](PROXMOX_SETUP.md) - Proxmox VE host preparation

## Support

- **Semaphore UI**: https://ansible.home.goodkind.io
- **Documentation**: See docs in this directory
