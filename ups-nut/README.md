# NUT (Network UPS Tools)

This directory stores NUT configuration planning and tooling for vault and suburban.

NUT is currently running manually on vault. No Ansible playbook exists yet. See
[PLAN.md](PLAN.md) for the full implementation roadmap.

## Hosts

- vault (`3d06:bad:b01::254`) is the NUT master, monitoring the primary UPS via a local
  driver and exposing upsd on the network.
- suburban (`3d06:bad:b01:200::254`) operates standalone or as a slave depending on
  network connectivity to vault.

## Status

Not yet Ansible-managed. The templates directory does not exist; configs are still
managed manually. See PLAN.md for the phased rollout plan.

## Credentials

NUT credentials will be stored in `ansible/inventory/group_vars/all/vault.yml`
(encrypted with Ansible Vault) once the playbook is built.
