# Ansible Configuration

Automated deployment and configuration management for goodkind.io infrastructure.

Playbooks run locally from this directory on the controller (typically this Mac).
Set the vault password at `~/.config/ansible/vault.pass` once; every command below
picks it up automatically through the `Rakefile` or the canonical
`ansible-playbook --vault-password-file` invocation.

## Inventory layout

Inventory is split by source type, all loaded together because `ansible.cfg` sets
`inventory = inventory` and Ansible walks the directory:

- `inventory/hosts`: static INI file. Owns the hypervisor SSH targets (`vault` in
  `[vault_servers]`, `suburban` in `[suburban_servers]`, joined by
  `[proxmox_servers:children]`) and the OPNsense SSH targets (`opnsense` in
  `[opnsense_servers]`, `opnsense-test` in `[opnsense_test_servers]`).
- `inventory/group_vars/all/service_mapping.yml`: single source of truth for
  container hostnames and IPv6 addresses. The custom `service_mapping` plugin reads
  it and creates one `<service>_servers` group per entry plus `all_services`.
- `inventory/vault.proxmox.yml`, `inventory/suburban.proxmox.yml`: one
  `community.proxmox.proxmox` plugin file per hypervisor. They each talk to their
  Proxmox API and emit the guests on that node as inventory hosts. The plugin
  requires filenames ending in `proxmox.yml`, which is why the per-hypervisor
  qualifier comes before the suffix.
- `inventory/group_vars/all/vars.yml`: shared non-secret defaults.
- `inventory/group_vars/all/vault.yml`: Ansible Vault-encrypted file holding every
  secret under a `vault_*` name. Playbooks and templates reference these names
  directly. Service-local wrapper variables are allowed only when they encode an
  environment-override-then-vault-fallback pattern such as
  `cloudflare_api_token: "{{ lookup('env', 'CLOUDFLARE_API_TOKEN') | default(vault_cloudflare_api_token, true) }}"`.

## Group_vars partitioning

Target-vars live in per-target group_vars files, not in environment bundles:

- `group_vars/proxmox_servers.yml`: shared Proxmox hypervisor defaults and feature
  flags consumed by `deploy-proxmox.yml`.
- `group_vars/vault_servers.yml`, `group_vars/suburban_servers.yml`: host-only vars
  for each hypervisor.
- `group_vars/mwan_servers.yml`, `group_vars/test_mwan_servers.yml`: MWAN VM
  target vars.
- `group_vars/mwan_failover_servers.yml`,
  `group_vars/mwan_failover_test_servers.yml`: MWAN failover LXC target vars.
- `group_vars/opnsense_servers.yml`, `group_vars/opnsense_test_servers.yml`:
  OPNsense SSH and identity vars.

`mwan_testbed_servers.yml` is a legacy free-standing include file that the old
testbed playbook loads via `include_vars`. It will be removed when those legacy
playbooks are deleted.

## Canonical playbooks

Five playbooks own the entire MWAN/Proxmox/OPNsense surface; each one targets
a specific group so prod-vs-testbed differences come from group_vars, not from
playbook conditionals:

- `playbooks/deploy-proxmox.yml`: Proxmox hypervisor configuration. Targets
  `proxmox_servers` (vault and suburban). Owns mwan-ifmgr, mwan-watchdog,
  cloudflared-oob, package-updater. Use `--limit vault` or `--limit suburban`.
- `playbooks/deploy-mwan.yml`: MWAN VM (prod VM 113 on vault). Targets
  `mwan_servers`. Builds the Go binary on the controller via
  `tasks/build-mwan-binary.yml`. Runtime network discovery lives in
  `tasks/mwan-vm/discover-runtime-network.yml`.
- `playbooks/deploy-mwan-failover.yml`: MWAN failover LXC. Targets
  `mwan_failover_servers` (prod) or `mwan_failover_test_servers` (testbed) via the
  union pattern; pick one with `--limit`.
- `playbooks/deploy-opnsense.yml`: mwan-opnsense daemon on the OPNsense host.
  Targets `opnsense_servers` (prod, SSH as `agoodkind` to `3d06:bad:b01::1`) or
  `opnsense_test_servers` (testbed, SSH as root through ProxyJump via suburban).
- `playbooks/deploy-testbed.yml`: suburban-only extras with no production
  equivalent. Includes `qm args` ownership for VM 950 and VM 101, ISP-LXC
  provisioning (200/201/202), mwan-opnsense Unix socket bridge, VFIO host-side
  passthrough. Targets `suburban_servers`.

## Where each task runs

Three different execution paths show up depending on what the task does:

- **Proxmox HTTP API.** Only the `community.proxmox.proxmox` dynamic inventory
  plugin uses the Proxmox HTTP API directly, to list guests. No other playbook
  contacts the Proxmox HTTP API for routine work.
- **Hypervisor SSH delegation.** Tasks that need to run `pct` or `qm` on a
  hypervisor open an SSH connection to that hypervisor and run there.
  `playbooks/deploy-ssh-keys.yml` is the canonical example: it uses
  `delegate_to: "{{ item.host }}"` with `pct push`/`pct exec` and `qm status`/
  `qm start`. The task "runs on" the hypervisor, not the guest.
- **Direct guest SSH.** Tasks that configure something inside a guest open SSH
  directly to the guest. `playbooks/prep-guests.yml` and most plays inside
  `deploy-mwan.yml` are direct-guest-SSH; the controller's SSH connection lands
  on the guest with no hypervisor in between.

When reading a playbook, look at `hosts:`, `delegate_to:`, and any `pct`/`qm`
commands to tell which path a task takes.

## Two Proxmox plugin files: name collisions

Each per-hypervisor Proxmox plugin file emits guests using the guest's raw `name`
field from Proxmox as the inventory hostname. The `community.proxmox.proxmox`
plugin offers no way to override this, so if a guest on vault and a guest on
suburban share the same Proxmox `name`, Ansible merges them into one inventory
host and the second-loaded plugin file wins on conflicting attributes
(`ansible_host` and similar). Today there are no collisions; if a future suburban
guest needs the same name as a vault guest, rename one of them in Proxmox itself.

## Secrets management

All secret values are stored in Ansible Vault under `vault_*` names. Files that
need a vault-stored secret reference the `vault_*` name directly. See
[SECRETS.md](SECRETS.md) for the naming rule, allowed env-wrapper exceptions, and
the safe key listing command. When you only need secret names, run
`python3 scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass ansible/inventory/group_vars/all/vault.yml`
from the repo root instead of `ansible-vault view`.

## Setup for new operators

Get the vault password from the team password manager and store it at
`~/.config/ansible/vault.pass` with `600` permissions. Install Ansible collections
with `ansible-galaxy collection install -r requirements.yml` before running
playbooks.

## Running playbooks

Direct invocation:

```bash
cd /Users/agoodkind/Sites/configs/ansible
ansible-playbook --vault-password-file ~/.config/ansible/vault.pass \
  playbooks/deploy-proxmox.yml --limit vault
```

Rake shortcuts (see `Rakefile` for the full list):

```bash
cd /Users/agoodkind/Sites/configs/ansible
rake help                          # all canonical shortcuts
rake deploy:proxmox[vault]         # full deploy, single host
rake check:mwan                    # --check --diff dry-run, all mwan_servers
rake syntax:all                    # syntax-check every canonical playbook
rake inventory                     # ansible-inventory --graph
```

## Documentation

- [Secrets Management](SECRETS.md)
- [Proxmox API Token Setup](PROXMOX_SETUP.md)
- [Semaphore UI](https://ansible.home.goodkind.io) (legacy)
