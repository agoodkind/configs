# Sysctl Configuration

The sysctl configuration for mwan has been moved to an Ansible template:

**Location:** `ansible/templates/mwan/sysctl-mwan.conf.j2`

**Why?** The configuration contains interface-specific settings that need to use the actual interface names (e.g., `eth1`, `eth2`), which vary based on hardware and VM configuration. By using a Jinja2 template, interface names are automatically populated from `group_vars/mwan_servers.yml`.

**Deployed to:** `/etc/sysctl.d/99-mwan.conf` on the mwan VM

The static file was removed to avoid confusion and ensure the template is the single source of truth.
