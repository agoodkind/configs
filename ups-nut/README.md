# NUT (Network UPS Tools)

This directory stores NUT configuration planning and tooling for vault and suburban.

NUT is deployed manually on vault and suburban. No Ansible playbook exists yet.
Each host monitors its own UPS independently.

## Hosts

- vault (`3d06:bad:b01::254`) monitors its local UPS.
- suburban (`10.240.0.148`, `3d06:bad:b01:200::1`) monitors its local UPS.

## Status

Not yet Ansible-managed. The live configs are manual and not in git.

## Credentials

NUT credentials should move to
[ansible/inventory/group_vars/all/vault.yml](../ansible/inventory/group_vars/all/vault.yml)
when a deploy playbook is added.

## Remaining work

1. Pull live configs from `/etc/nut/` on vault and suburban.
2. Commit host-specific templates under this directory.
3. Add an Ansible playbook for reproducible deployment.
