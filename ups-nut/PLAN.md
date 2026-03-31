# NUT Configuration Plan

NUT is already deployed and running in standalone mode on both vault and suburban. Each
host monitors its own UPS independently. Standalone is the correct topology since the
two hosts are on opposite coasts with separate UPS hardware.

The configs are manual and not in git. If either host is rebuilt, the NUT setup would
need to be recreated from scratch.

## Remaining work

1. Pull live configs from `/etc/nut/` on vault and suburban and commit them to
   `ups-nut/vault/` and `ups-nut/suburban/`.
2. Templatize the captured configs (Jinja2) to make future deployments reproducible.
3. Write `deploy-nut.yml` to deploy the templates via Ansible.
