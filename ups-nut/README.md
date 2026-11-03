# NUT (Network UPS Tools)

This directory stores NUT configuration planning and tooling for vault and suburban.

Each host monitors its own UPS independently.

## Hosts

- vault (`3d06:bad:b01::254`) monitors its local UPS.
- suburban (`10.240.0.148`, `3d06:bad:b01:200::1`) monitors its local UPS.

## Status

Not yet Ansible-managed. The live configs are manual and not in git.

## Remaining work

1. Pull live configs from `/etc/nut/` on vault and suburban.
2. Commit host-specific templates under this directory.
