# Ansible configuration

Automated deployment and configuration management for goodkind.io infrastructure.

Playbooks run from the controller via
the configs binary or the Rake
helpers, both of which pick up `~/.config/ansible/vault.pass`. The canonical
deploy invocation lives in [AGENTS.md](../../AGENTS.md). See
[docs/ansible/secrets.md](secrets.md) for the vault contract,
[docs/ansible/quality.md](quality.md) for style and safety rules, and
[docs/ansible/proxmox-api.md](proxmox-api.md) for Proxmox API token setup.

## Inventory layout

[ansible/ansible.cfg](../../ansible/ansible.cfg) sets `inventory = inventory`,
so Ansible walks the directory and merges every source. The inventory is
partitioned by source type, not by host group:

- [ansible/inventory/hosts](../../ansible/inventory/hosts): static INI file.
  Owns the hypervisor parent groups (`vault`, `suburban`) and the testbed
  groups for hosts the Proxmox API does not surface. It does not own the
  OPNsense SSH targets.
- [ansible/inventory/group_vars/all/service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml):
  single source of truth for service hostnames, IPv6 addresses, and optional
  `ansible_host` overrides. A custom `service_mapping` inventory plugin (source
  at [ansible/plugins/inventory/service_mapping.py](../../ansible/plugins/inventory/service_mapping.py))
  reads it and creates one `<service>_servers` group per entry plus an
  `all_services` group.
- [ansible/inventory/*.proxmox.yml](../../ansible/inventory/):
  one `community.proxmox.proxmox` plugin file per hypervisor. Each talks to
  its Proxmox API and emits that node's guests as inventory hosts. The plugin
  requires filenames ending in `proxmox.yml`, which is why the per-hypervisor
  qualifier comes first.
- [ansible/inventory/group_vars/all/vars.yml](../../ansible/inventory/group_vars/all/vars.yml):
  shared non-secret defaults.
- [ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml):
  Ansible Vault-encrypted file holding every secret under a `vault_*` name.
  Playbooks and templates reference these names directly.
  See [docs/ansible/secrets.md](secrets.md) for the full contract.

Target-specific variables live in files under
[ansible/inventory/group_vars/](../../ansible/inventory/group_vars/), named
`<group>_servers.yml`. Treat the directory listing as the authoritative
inventory of those files; the set changes as services come and go and is not
enumerated here.

## OPNsense inventory ownership

OPNsense is a special case worth calling out, because it is the easiest place
to introduce drift:

- The `opnsense` and `opnsense_test` entries in
  [service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml)
  create the `opnsense_servers` and `opnsense_test_servers` groups via the
  `service_mapping` plugin. There is no static `[opnsense_servers]` group in
  [inventory/hosts](../../ansible/inventory/hosts).
- The plugin sets `ansible_host` from the canonical IPv6 by default. The
  `opnsense_test` entry overrides `ansible_host` to its LAN IPv4 because the
  testbed IPv6 is not reachable from the controller.
- Connection vars (SSH user, ProxyJump, BGP identity, gateway names, etc.) live
  in
  [ansible/inventory/group_vars/opnsense_servers.yml](../../ansible/inventory/group_vars/opnsense_servers.yml)
  and
  [ansible/inventory/group_vars/opnsense_test_servers.yml](../../ansible/inventory/group_vars/opnsense_test_servers.yml).

[ansible/playbooks/deploy-opnsense.yml](../../ansible/playbooks/deploy-opnsense.yml)
runs against `opnsense_servers:opnsense_test_servers` and is branchless on
`inventory_hostname`. All connection differences are absorbed by the two
group_vars files.

## Where each task runs

Three execution paths show up depending on what a task does:

- **Proxmox HTTP API.** Only the `community.proxmox.proxmox` dynamic inventory
  plugin uses the Proxmox HTTP API directly, to list guests. No routine
  playbook contacts the Proxmox HTTP API.
- **Hypervisor SSH delegation.** Tasks that need to run `pct` or `qm` on a
  hypervisor open SSH to that hypervisor and run there.
  [ansible/playbooks/deploy-ssh-keys.yml](../../ansible/playbooks/deploy-ssh-keys.yml)
  is the canonical example: it uses `delegate_to: "{{ item.host }}"` with `pct
  push`, `pct exec`, and `qm` status/start. The task runs on the hypervisor,
  not the guest.
- **Direct guest SSH.** Tasks that configure something inside a guest open SSH
  directly to the guest.
  [ansible/playbooks/prep-guests.yml](../../ansible/playbooks/prep-guests.yml)
  and most plays inside
  [ansible/playbooks/deploy-mwan.yml](../../ansible/playbooks/deploy-mwan.yml)
  are direct-guest-SSH; the controller's SSH connection lands on the guest
  with no hypervisor in between.

When reading a playbook, look at `hosts:`, `delegate_to:`, and any `pct` or
`qm` commands to tell which path a task takes.

## Proxmox plugin name collisions

Each per-hypervisor Proxmox plugin file emits guests using the guest's raw
`name` field from Proxmox as the inventory hostname. The
`community.proxmox.proxmox` plugin offers no way to override this. If a guest
on one hypervisor shares its Proxmox `name` with a guest on another, Ansible
merges them into one inventory host, and the second-loaded plugin file wins on
conflicting attributes such as `ansible_host`. When this happens, rename one
of the guests in Proxmox itself.

## Secrets management

All secret values live in Ansible Vault under `vault_*` names. Files that need
a vault-stored secret reference the `vault_*` name directly. See
[docs/ansible/secrets.md](secrets.md) for the naming rule, allowed env-wrapper
exceptions, and the safe key listing command.

## Setup for new operators

Store the team vault password at `~/.config/ansible/vault.pass` with mode
`600`. Install Ansible collections with
`ansible-galaxy collection install -r playbooks/requirements.yml` using
[ansible/playbooks/requirements.yml](../../ansible/playbooks/requirements.yml).

## Running playbooks

The canonical deploy invocation lives in [AGENTS.md](../../AGENTS.md). Rake
helpers wrap the canonical playbooks. See
[ansible/Rakefile](../../ansible/Rakefile) for the current set.
