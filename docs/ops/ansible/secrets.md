# Ansible vault secrets

Every shared inventory secret is Ansible Vault-encrypted inside the inventory, one key
per secret, and every key name starts with `vault_`. Consumers reference the
`vault_` name directly. Pure aliases like `api_key: "{{ vault_api_key }}"` are
banned. An env-wrapper variable is allowed only when it encodes a real
environment override with a vault fallback and stays local to the service that
needs it; the Cloudflare token wrapper in the proxy group vars is the one
current example.

configsctl is the only sanctioned interface to the vault. Do not
invoke `ansible`, `ansible-vault`, `ansible-playbook`, `ansible-inventory`, or
`ansible-console` directly.

List key names without decrypting any value:

```bash
./configsctl keys
```

Add or rotate secrets by piping a YAML mapping of key to value into
`configsctl set-secrets`, which merges the mapping into the vault and prints only
the added and updated key names. Read a single value with
`configsctl secret <key>`, which writes it to a tool-owned temp file and prints
the cleanup command; never cat, paste, log, or commit that file.

The vault password sits at `~/.config/ansible/vault.pass` with mode `600`.
Every configsctl invocation reads it from there. If decryption fails, verify that
file exists and holds the current password. If a playbook reports an undefined
`vault_` variable, the key is missing from the vault or the consumer still
references a removed alias.

If the password is lost, the vault cannot be decrypted. Restore the password
from 1Password, or re-create every secret from its original source into a
fresh vault. Keep the password backed up in 1Password.

The OpenTofu `cloudflare.mwan_manage` provider reads a separate local token from
`~/.config/mwan/cloudflare-lb-manage.token`. Protect the directory with mode `700`
and the file with mode `600`. Keep its token ID, permissions and expiration
in private metadata beside the token. Set `TF_VAR_cloudflare_mwan_manage_token_file`
to another protected file when required. Grant account-scoped monitor and pool
read/write permissions and zone-scoped load balancer read/write permissions.
The default Cloudflare provider
continues to use `CLOUDFLARE_API_TOKEN` for DNS and other resources.

Run OpenTofu through `./configsctl tofu`. The wrapper inherits the file path
override. Rotate the persistent token before its recorded expiration and verify
an unchanged targeted plan after rotation. Preserve the DNS vault token and
the separate `~/.config/mwan/cloudflare-lb-read.token` health observation credential.
