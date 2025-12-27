# Ansible Vault - Quick Reference

## Common Commands

```bash
# View secrets
ansible-vault view inventory/group_vars/all/vault.yml

# Edit secrets (recommended)
ansible-vault edit inventory/group_vars/all/vault.yml

# Add a new secret
ansible-vault edit inventory/group_vars/all/vault.yml
# Add: vault_new_secret: "value"
# Then reference in vars.yml: new_secret: "{{ vault_new_secret }}"

# Decrypt temporarily
ansible-vault decrypt inventory/group_vars/all/vault.yml
# Edit file
ansible-vault encrypt inventory/group_vars/all/vault.yml

# Change vault password
ansible-vault rekey inventory/group_vars/all/vault.yml
```

## File Locations

- **Vault file**: `inventory/group_vars/all/vault.yml` (encrypted, in git)
- **Vars file**: `inventory/group_vars/all/vars.yml` (not encrypted, in git)
- **CLI password**: `~/.config/ansible/vault.pass` (not in git)
- **Semaphore password**: Environment variable `ANSIBLE_VAULT_PASSWORD` in database

## Naming Convention

- Secrets in vault.yml: `vault_*` prefix
- References in vars.yml: no prefix, use `{{ vault_* }}`

## Example

**vault.yml**:

```yaml
vault_api_key: "secret123"
```

**vars.yml**:

```yaml
api_key: "{{ vault_api_key }}"
```

**Usage in playbook**:

```yaml
- name: Call API
  uri:
    url: https://api.example.com
    headers:
      Authorization: "Bearer {{ api_key }}"
```

## Troubleshooting

| Error | Fix |
|-------|-----|
| Decryption failed | Check `~/.config/ansible/vault.pass` exists and is correct |
| Variable undefined | Add variable to vault.yml with `ansible-vault edit` |
| Semaphore fails | Verify `ANSIBLE_VAULT_PASSWORD` env var in Semaphore environment |

## Emergency: Lost Vault Password

If you lose the vault password, you **cannot** decrypt vault.yml. Options:

1. **Restore from backup** (if you have one)
2. **Re-create all secrets** from original sources and create new vault.yml
3. **Check Semaphore database** for old encrypted secrets (may still have them)

**Prevention**: Back up vault password in team password manager (1Password, etc.)
