# Proxmox API Token Setup for Ansible

This document describes the required permissions for the `ansible@pam` API token.

## Create API Token

1. In Proxmox web UI, go to **Datacenter** → **Permissions** → **API Tokens**
2. Create token for user `ansible@pam`:
   - Token ID: `ansible-token`
   - Privilege Separation: **Unchecked** (use user permissions)

## Required Permissions

Grant the following permissions to `ansible@pam` on path `/`:

```bash
# On Proxmox host, run:
pveum acl modify / -user ansible@pam -role PVEVMAdmin
```

This grants:
- `VM.Allocate` - Create/destroy VMs/containers
- `VM.Config.Options` - Modify container options (including features like nesting)
- `VM.Config.Disk` - Manage disks
- `VM.Config.CPU` - Manage CPU settings
- `VM.Config.Memory` - Manage memory settings
- `VM.Config.Network` - Manage network settings
- `VM.PowerMgmt` - Start/stop/reboot
- `VM.Console` - Access console
- `VM.Audit` - View configuration
- `Datastore.AllocateSpace` - Allocate storage

## Verify Permissions

```bash
pveum user permissions ansible@pam
```

Should show `PVEVMAdmin` role on `/` path.

## Store Token in Environment

On the ansible container (`ansible.home.goodkind.io`):

```bash
# Add to ~/.bashrc
echo 'export PROXMOX_API_TOKEN="<your-token>"' >> ~/.bashrc
source ~/.bashrc
```

Or use the helper script:

```bash
./setup-api-token.sh
```

## Troubleshooting

### Permission denied (403) errors

If you see errors like:
```
Permission check failed (changing feature flags (except nesting) is only allowed for root@pam)
```

The `ansible@pam` user needs `VM.Config.Options` permission. Grant it with:

```bash
pveum acl modify / -user ansible@pam -role PVEVMAdmin
```

### Alternative: Use root@pam (not recommended)

For testing only, you can use `root@pam` instead of `ansible@pam`:

1. Create API token for `root@pam`
2. Update `ansible/inventory/group_vars/all.yml`:
   ```yaml
   proxmox_api_user: root@pam
   proxmox_token_id: root-ansible-token
   ```

**Note**: Using `root@pam` is less secure. Prefer granting specific permissions to `ansible@pam`.

