# Proxmox API Token Setup for Ansible

## Create the API token

In the Proxmox web UI, go to Datacenter, then Permissions, then API Tokens. Create a
token for `ansible@pam` with token ID `ansible-token`. Uncheck "Privilege Separation"
so the token inherits the user's permissions directly.

## Required permissions

Grant `ansible@pam` the `PVEVMAdmin` role on path `/`, either through the Proxmox ACL
UI or by running `pveum acl modify` on the Proxmox host. This role covers VM and
container create/destroy, config changes (options, disk, CPU, memory, network), power
management, console access, and datastore allocation.

To verify, check the ACL list and confirm `ansible@pam` has `PVEVMAdmin` on `/`.

## Store the token

On the ansible container, export `PROXMOX_API_TOKEN` in `~/.bashrc`. In Semaphore,
add it as an environment variable in the project environment.

## Troubleshooting

Permission denied (403) errors mentioning "changing feature flags (except nesting)" mean
`ansible@pam` is missing `VM.Config.Options`. Re-grant `PVEVMAdmin` on `/` to fix it.

Using `root@pam` instead of `ansible@pam` works but is less secure and should be limited
to testing. If you do, update `proxmox_api_user` and `proxmox_token_id` in the inventory
group vars.
