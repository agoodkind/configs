# OpenTofu state operations

## Backend

OpenTofu stores its state in the Cloudflare R2 bucket named tofu-state. The
bucket is outside the homelab and survives the loss of a hypervisor or a
workstation. The backend uses the S3 protocol with a lock file. A second run
waits for the lock.

Run every tofu command through configsctl. configsctl reads the credentials
from the Ansible vault.

```bash
./configsctl tofu plan
./configsctl tofu apply
```

A fresh checkout needs one `./configsctl tofu init` before its first plan.

Each directory under the workspaces directory with a `backend` block is a
workspace with its own state file. A workspace name before the tofu command
selects that workspace.

```bash
./configsctl tofu guest plan
```

Each workspace needs its own `init`.

Run [opentofu/guest/install-providers.sh](guest/install-providers.sh) before the first `./configsctl tofu guest init` in a checkout. The script installs both pinned providers into the guest workspace's implied local mirror.

```bash
opentofu/guest/install-providers.sh
./configsctl tofu guest init
```

Run the script again after a change to [providers.pin](guest/providers.pin). After a new release under the unchanged provider version `0.1.0`, delete `opentofu/guest/.terraform.lock.hcl` before running `./configsctl tofu guest init` again.

## Secrets

configsctl exports only the vault keys that match one of two rules.

| Vault key | OpenTofu receives |
| --- | --- |
| Same name as a variable declared in the selected workspace | That variable |
| `vault_tofu_env_<NAME>` | The environment variable `<NAME>` |

The S3 backend reads its credentials from the environment variables
`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. The vault keys
`vault_tofu_env_AWS_ACCESS_KEY_ID` and `vault_tofu_env_AWS_SECRET_ACCESS_KEY`
supply them.

To change the workspaces directory (`workspaces_dir`) or the `vault_tofu_env_`
prefix, edit [configsctl.yml](../configsctl.yml).

## Add a secret

1. Add the vault key with `./configsctl set-secrets`.
2. Declare a variable with the same name in the workspace that reads the
   secret.

## Rotate the backend credential

1. Create a Cloudflare API token with the Workers R2 Storage Write permission.
2. Derive the S3 pair. The access key id is the token id. The secret is the
   SHA-256 hex digest of the token value.
3. Write both `vault_tofu_env_AWS_*` keys with `./configsctl set-secrets`.
4. Revoke the old token in the Cloudflare dashboard.

## State encryption

OpenTofu encrypts the state file in R2 and every saved plan file. OpenTofu
derives the key from the vault key `vault_tofu_state_passphrase`.

A checkout without the encryption configuration fails every tofu command.
Merge main into that checkout.

A lost passphrase makes the state unreadable. The Ansible vault stores the
passphrase, and 1Password stores the vault password.

Do not rename the `state` key provider or the `state` method in
[encryption.tf](encryption.tf). OpenTofu writes that name into the encrypted
data.

## Attach an existing resource

Confirm that the OpenTofu configuration matches the live object before
importing it. Read a guest with `qm config <vmid>` or `pct config <vmid>`.
Compare its VMID, storage, network devices, and hardware with the configured
resource.

The Proxmox provider uses these import identifiers:

- Network interfaces use `<node_name>:<interface>`.
- Virtual machines and containers use `<node_name>/<vmid>`.

Import the live object through the repo control tool:

```bash
./configsctl tofu import \
  '<resource_address>' '<provider_import_id>'
```

Run a complete plan after import. Review every difference using the drift rules
below before applying anything.

## Reattach a renumbered guest

Proxmox treats the virtual machine identifier (VMID) as the guest identity.
OpenTofu cannot update `vm_id` in place. Renumber the live guest, then reattach
its state. Never destroy and recreate the guest for a VMID change.

For a ZFS-backed guest, renaming each dataset preserves its child snapshots.

1. Stop the guest.
2. Rename every ZFS dataset from the old VMID to the new VMID.
3. Update every volume reference in the guest configuration. Update the active
   configuration and every `[snapname]` section.
4. Move the configuration to the new VMID, then start the guest.
5. Remove the resource from state, then import it with the new VMID:

```bash
./configsctl tofu state rm '<resource_address>'
./configsctl tofu import \
  '<resource_address>' '<node_name>/<new_vmid>'
```

Do not use `tofu state mv`. That command changes the resource address but leaves
the old `vm_id` in state. The next plan then proposes a replacement.

## Review drift before applying

Keep `lifecycle.prevent_destroy = true` on managed Proxmox resources. The guard
blocks deletes and replacements. It does not block a destructive update in
place. Add the guard to every newly imported resource.

Read the attribute changes in every plan. The change count does not distinguish
provider bookkeeping from a hardware change. Treat changes to `cpu`, `memory`,
and `disk` as live hardware changes that require confirmation.

A fresh import can add provider defaults such as `timeout_*` values and blocks
that state could not populate. Review those separately from hardware changes.

Configuration must not understate a live disk. Proxmox cannot shrink a
container or virtual machine disk safely. Update OpenTofu when a disk grows on
the hypervisor, or a later plan can propose a destructive shrink.

After repairing a plan failure, read the complete plan again. Hidden drift can
accumulate while plans remain broken.

The provider has these expected readback gaps:

- Ansible owns the live `args` field on the MWAN and OPNsense virtual machines.
  The Proxmox API rejects token writes to that field, so OpenTofu ignores
  `kvm_arguments` instead of removing the live value. The MWAN value sets its
  virtual socket context identifier, which tracks its VMID. The OPNsense value
  serves the out-of-band serial channel.
- Proxmox does not return injected SSH keys. Resources with a configured
  `initialization.user_account` ignore that block so a reimport does not force a
  replacement.
- Proxmox does not store the source template name in `pct config`. Imported
  containers ignore `operating_system.template_file_id`.
- The object stores' disks moved to each hypervisor's slow storage tier with
  `pct move-volume`, and the tack owner guests' backup roots are volumes hot
  plugged with `pct set`. The provider can make neither change without
  replacing the guest, so those containers ignore `disk[0].datastore_id` or
  `mount_point`.
- Ansible owns `/etc/network/interfaces.d/testbed-masquerade.conf` and the extra
  routable IPv6 address on `vmbr1`.
- A container state `id` contains only the VMID. Match a container by both
  `node_name` and `id` when inspecting state across hypervisors. A VMID alone
  does not identify its hypervisor.
