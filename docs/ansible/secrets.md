# Ansible Vault Quick Reference

All secret values live in [ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml), encrypted with Ansible
Vault. [ansible/inventory/group_vars/all/vars.yml](../../ansible/inventory/group_vars/all/vars.yml) stores shared non-secret variables only.
List vault key names with
the configs binary `keys`. Do not
invoke `ansible`, `ansible-vault`, `ansible-playbook`, `ansible-inventory`, or
`ansible-console` directly.

## File locations

- Vault file: [ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml) (encrypted, committed to git)
- Shared vars file: [ansible/inventory/group_vars/all/vars.yml](../../ansible/inventory/group_vars/all/vars.yml) (plaintext, committed to git)
- CLI password: `~/.config/ansible/vault.pass` (not in git, must be 600 permissions)

## Safe key listing

```bash
go run goodkind.io/configs/cmd/configs keys
```

Prints key paths such as `vault_proxmox_token_secret`. Decrypted values are not
printed.

## Variable contract

- Secret values in [ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml) use `vault_*` names.
- [ansible/inventory/group_vars/all/vars.yml](../../ansible/inventory/group_vars/all/vars.yml) stores shared non-secret values only.
- Files that need a vault-stored secret reference the `vault_*` name directly.
- Do not add pure aliases like `api_key: "{{ vault_api_key }}"` to shared or service `group_vars` files.
- Env-wrapper variables are allowed only when they encode a real env override with a vault fallback and stay local to the service or play that needs them.
- Example: `cloudflare_api_token` in [ansible/inventory/group_vars/proxy_servers.yml](../../ansible/inventory/group_vars/proxy_servers.yml) is allowed because it checks `CLOUDFLARE_*` environment variables first and falls back to `vault_cloudflare_api_token`.

## Troubleshooting

If decryption fails, verify `~/.config/ansible/vault.pass` exists and contains the
correct password. If a playbook reports an undefined `vault_*` variable, verify that
the key exists in [ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml) and that the consumer is not still using a removed alias.

## Lost vault password

If the vault password is lost, decryption of
[ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml)
is not possible. Options are to restore the password from the team password manager
(1Password), or to re-create all secrets from their original sources and create a new
[ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml).
Back up the vault password in the team password manager.
