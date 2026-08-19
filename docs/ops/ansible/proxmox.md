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

## Rotate a token

The per-hypervisor inventory plugins keep the API URL, username, and token ID
in plaintext and read the token secret from the vault. When a token rotates,
update `vault_proxmox_token_secret` for the production hypervisor or
`vault_suburban_testbed_pve_token_secret` for suburban with
`configs set-secrets`. Do not move the token secrets into shell startup files
just to satisfy inventory.

## Verification

Check the ACL list and confirm `ansible@pam` has `PVEVMAdmin` on `/`. Then run
a read-only inventory load and confirm the Proxmox plugin can list guests
without an authentication failure.

## Troubleshooting

Permission denied (`403`) errors that mention changing feature flags usually
mean `ansible@pam` is missing `VM.Config.Options`. Re-grant `PVEVMAdmin` on `/`
and retry.

Using `root@pam` instead of `ansible@pam` can work for diagnostics, but it is a
separate operational path and should not become the documented default.
