# Ansible

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
