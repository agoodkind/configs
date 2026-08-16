# Ansible

Ansible configures every guest in the homelab from a single controller. It
takes a freshly provisioned container or virtual machine, brings it up to a
running and deployable state, and keeps it there as the fleet changes.
Playbooks run through the configs binary or the Rake helpers; the repository
guide carries the canonical deploy invocation.

## Inventory layout

Ansible walks the inventory directory and merges every source, so the
inventory is partitioned by source type, not by host group.

A static hosts file owns the hypervisor parent groups (`vault`, `suburban`)
and the testbed groups the Proxmox API does not surface. A custom
service-mapping plugin reads the shared service inventory and creates one
`<service>_servers` group per entry plus an `all_services` group, setting each
host's `ansible_host` from its canonical IPv6. One Proxmox plugin file per
hypervisor talks to that node's API and emits its guests as inventory hosts;
the plugin only loads files whose names end in `proxmox.yml`, so name a new
per-hypervisor file with that suffix and put the hypervisor qualifier first.
Shared non-secret defaults and the encrypted vault complete the merge; the
secret contract is in [secrets.md](ops/ansible/secrets.md).

Target-specific variables sit in one per-group file per service, named
`<group>_servers.yml`. The directory listing is the authoritative set; it
changes as services come and go and is not enumerated here.

## OPNsense inventory ownership

OPNsense is the easiest place to introduce inventory drift. Its two service
entries create the `opnsense_servers` and `opnsense_suburban_servers` groups
through the service-mapping plugin; no static group exists for them. The
plugin sets `ansible_host` from the canonical IPv6 by default, and the
suburban entry selects its routed transit address instead. The SSH user, BGP
identity, and gateway names sit in the two OPNsense per-group files. The
OPNsense deploy runs against `opnsense_servers:opnsense_suburban_servers` and
connects directly to each routed address; the play is branchless on
`inventory_hostname`.

## Where each task runs

Three execution paths show up depending on what a task does:

- **Proxmox HTTP API.** Only the dynamic inventory plugin uses the Proxmox
  HTTP API, to list guests. No routine playbook contacts it.
- **Hypervisor SSH delegation.** Tasks that need `pct` or `qm` open SSH to the
  hypervisor and run there. The SSH-key deploy is the canonical example: it
  delegates to the hypervisor and pushes into guests with `pct push` and
  `pct exec`. The task runs on the hypervisor, not the guest.
- **Direct guest SSH.** Tasks that configure something inside a guest open SSH
  directly to the guest, with no hypervisor in between. Guest prep and most of
  the MWAN deploy work this way.

When reading a playbook, look at `hosts:`, `delegate_to:`, and any `pct` or
`qm` commands to tell which path a task takes.

## Proxmox plugin name collisions

Each per-hypervisor plugin file emits guests under the guest's raw Proxmox
`name` as the inventory hostname, with no override available. If a guest on
one hypervisor shares its name with a guest on another, Ansible merges them
into one inventory host, and the second-loaded plugin wins on conflicting
attributes such as `ansible_host`. When this happens, rename one of the
guests in Proxmox itself.

## Setup for new operators

Set up vault access per the secret contract, then install the Ansible
collections:

```bash
ansible-galaxy collection install -r playbooks/requirements.yml
```
