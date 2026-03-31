# Ansible Vault - Quick Reference

All secrets live in `inventory/group_vars/all/vault.yml`, encrypted with Ansible Vault.
Edit it with `ansible-vault edit`, view it with `ansible-vault view`, and rekey it with
`ansible-vault rekey` when rotating the vault password.

## File locations

- Vault file: `inventory/group_vars/all/vault.yml` (encrypted, committed to git)
- Vars file: `inventory/group_vars/all/vars.yml` (plaintext, committed to git)
- CLI password: `~/.config/ansible/vault.pass` (not in git, must be 600 permissions)
- Semaphore password: environment variable `ANSIBLE_VAULT_PASSWORD` in the Semaphore database

## Naming convention

Secrets in `vault.yml` use a `vault_` prefix. They are exposed in `vars.yml` without
the prefix so playbooks reference clean names. For example, a secret named
`vault_api_key` in `vault.yml` would be referenced as `api_key` elsewhere by
assigning `api_key: "{{ vault_api_key }}"` in `vars.yml`.

## Troubleshooting

If decryption fails, verify `~/.config/ansible/vault.pass` exists and contains the
correct password. If a variable shows as undefined in a playbook, the variable is likely
absent from `vault.yml` or `vars.yml`. If Semaphore fails, check that
`ANSIBLE_VAULT_PASSWORD` is set in the Semaphore project environment.

## Lost vault password

If the vault password is lost, decryption of `vault.yml` is not possible. Options are
to restore the password from the team password manager (1Password), or to re-create all
secrets from their original sources and create a new `vault.yml`. Back up the vault
password in the team password manager.
