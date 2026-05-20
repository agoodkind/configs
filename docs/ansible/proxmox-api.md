# Proxmox API token setup for Ansible

## Create the API token

In the Proxmox web UI, go to Datacenter, then Permissions, then API Tokens.
Create a token for `ansible@pam` with token ID `ansible-token`, and disable
Privilege Separation so the token inherits the user's permissions directly.

## Required permissions

Grant `ansible@pam` the `PVEVMAdmin` role on path `/`. That role covers guest
lifecycle operations, config changes, console access, and datastore allocation.
If a workflow needs a narrower ACL later, document the missing capability before
you tighten it.

## Where Ansible reads the token secrets

The per-hypervisor inventory plugins keep the API URL, username, and token ID in
plaintext, and they read the token secret directly from
[ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml):

- [ansible/inventory/vault.proxmox.yml](../../ansible/inventory/vault.proxmox.yml) reads `vault_proxmox_token_secret`
- [ansible/inventory/suburban.proxmox.yml](../../ansible/inventory/suburban.proxmox.yml) reads `vault_suburban_testbed_pve_token_secret`

Update those `vault_*` values when a token rotates. Do not move the token
secrets into shell startup files just to satisfy inventory.

## Verification

Check the ACL list and confirm `ansible@pam` has `PVEVMAdmin` on `/`. Then run a
read-only inventory load from [ansible/](../../ansible/) and confirm the
Proxmox plugin can list guests without an authentication failure.

## Troubleshooting

Permission denied (`403`) errors that mention changing feature flags usually
mean `ansible@pam` is missing `VM.Config.Options`. Re-grant `PVEVMAdmin` on `/`
and retry.

Using `root@pam` instead of `ansible@pam` can work for diagnostics, but it is a
separate operational path and should not become the documented default.
