# Ansible Vault Quick Reference

All secret values live in `inventory/group_vars/all/vault.yml`, encrypted with Ansible
Vault. `inventory/group_vars/all/vars.yml` stores shared non-secret variables only.
Edit the vault with `ansible-vault edit`, and rekey it with `ansible-vault rekey` when
rotating the vault password. For safe key discovery in transcripted workflows, use
`python3 scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass ansible/inventory/group_vars/all/vault.yml`
instead of `ansible-vault view`.

## File locations

- Vault file: `inventory/group_vars/all/vault.yml` (encrypted, committed to git)
- Shared vars file: `inventory/group_vars/all/vars.yml` (plaintext, committed to git)
- CLI password: `~/.config/ansible/vault.pass` (not in git, must be 600 permissions)
- Semaphore password: environment variable `ANSIBLE_VAULT_PASSWORD` in the Semaphore database

## Safe key listing

From the repo root, list only vault key names with:

```bash
python3 scripts/ansible_vault_keys.py \
  --vault-password-file "$HOME/.config/ansible/vault.pass" \
  ansible/inventory/group_vars/all/vault.yml
```

This command prints only key paths such as `vault_proxmox_token_secret`. It does not
print decrypted values.

## Variable contract

- Secret values in `vault.yml` use `vault_*` names.
- `inventory/group_vars/all/vars.yml` stores shared non-secret values only.
- Files that need a vault-stored secret reference the `vault_*` name directly.
- Do not add pure aliases like `api_key: "{{ vault_api_key }}"` to shared or service `group_vars` files.
- Env-wrapper variables are allowed only when they encode a real env override with a vault fallback and stay local to the service or play that needs them.
- Example: `cloudflare_api_token` in `inventory/group_vars/proxy_servers.yml` is allowed because it checks `CLOUDFLARE_*` environment variables first and falls back to `vault_cloudflare_api_token`.

## Troubleshooting

If decryption fails, verify `~/.config/ansible/vault.pass` exists and contains the
correct password. If a playbook reports an undefined `vault_*` variable, verify that
the key exists in `vault.yml` and that the consumer is not still using a removed alias.
If Semaphore fails, check that `ANSIBLE_VAULT_PASSWORD` is set in the Semaphore project
environment.

## Lost vault password

If the vault password is lost, decryption of `vault.yml` is not possible. Options are
to restore the password from the team password manager (1Password), or to re-create all
secrets from their original sources and create a new `vault.yml`. Back up the vault
password in the team password manager.
