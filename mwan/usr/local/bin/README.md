# Helper Scripts

The following scripts have been moved to Ansible templates for automated interface name substitution:

- **update-routes.sh** → `ansible/templates/mwan/update-routes.sh.j2`
- **health-check.sh** → `ansible/templates/mwan/health-check.sh.j2`

**Why?** These scripts contain interface-specific logic that needs to use the actual interface names from `group_vars/mwan_servers.yml`. By templating them, interface names are automatically populated during deployment.

**Still static:**

- **update-npt.sh** → Remains in `mwan/usr/local/bin/update-npt.sh` (no interface-specific logic)

**Deployed to:** `/usr/local/bin/` on the mwan VM by `deploy-mwan.yml`
