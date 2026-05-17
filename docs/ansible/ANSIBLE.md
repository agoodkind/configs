# Ansible Configuration

Automated deployment and configuration management for goodkind.io infrastructure.

Playbooks run from this directory, either on the CLI from the ansible container at
`3d06:bad:b01::107` (which has `PROXMOX_API_TOKEN` set), or via the
[Semaphore UI](https://ansible.home.goodkind.io). The vault password lives at
`~/.config/ansible/vault.pass` on the controller and as `ANSIBLE_VAULT_PASSWORD` in
the Semaphore environment.

## Key inventory files

`inventory/group_vars/all/service_mapping.yml` is the single source of truth for
container hostnames and IPv6 addresses. The dynamic inventory plugin and all templates
derive from it. Non-secret variables live in `vars.yml` alongside it. Secrets live in
`vault.yml`, encrypted with Ansible Vault.

## Secrets Management

All secrets are stored in Ansible Vault. See [SECRETS.md](SECRETS.md) for vault
commands, the naming convention, and the safe key listing command. When you only need
secret names, run `python3 scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass ansible/inventory/group_vars/all/vault.yml`
from the repo root instead of `ansible-vault view`.

## Setup for new operators

Get the vault password from the team password manager and store it at
`~/.config/ansible/vault.pass` with `600` permissions. Install Ansible collections
with `ansible-galaxy collection install -r requirements.yml` before running playbooks.

## Documentation

- [Secrets Management](SECRETS.md)
- [Proxmox API Token Setup](PROXMOX_SETUP.md)
- [Semaphore UI](https://ansible.home.goodkind.io)
