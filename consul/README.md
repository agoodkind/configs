# Consul Service Discovery

This directory contains Consul service discovery configuration for the infrastructure.

## Architecture

- **Consul Server**: Single LXC container (`consul.home.goodkind.io`, `3d06:bad:b01::106`), `bootstrap_expect=1`, datacenter `home`, domain `int`.
- **Consul Agents**: Deployed to all LXC containers via `prep-guests.yml`.
- **Service Discovery**: Ansible queries the HTTP API at deploy time to build Traefik/SSHPiper configs; services use `*.service.int` DNS at runtime via systemd-resolved forwarding.
- **Fallback**: Static IPs from `inventory/group_vars/all/service_mapping.yml` are included in templates so configs work even if Consul is down.

## Deployment Order

```bash
# 1. Deploy Consul server
ansible-playbook playbooks/deploy-consul.yml

# 2. Verify server is up
ssh consul@ssh.home.goodkind.io consul members

# 3. Re-run prep-guests to install agents on existing containers
ansible-playbook playbooks/prep-guests.yml

# 4. Redeploy services to register with Consul
ansible-playbook playbooks/deploy-adguard.yml
ansible-playbook playbooks/deploy-proxy.yml

# 5. Deploy agents to external hosts (vault, NAS, mini, OPNsense)
ansible-playbook playbooks/deploy-consul-external.yml
```

Note: `deploy-consul-external.yml` currently has `consul_arch: arm64` hardcoded.
Target hosts are `amd64`. This must be corrected before running.

## Directory Structure

```
consul/
├── README.md                           # This file
├── consul-server.hcl.j2                # Consul server configuration
├── consul-agent.hcl.j2                 # Consul agent configuration
├── consul.service                      # systemd service unit
├── services/
│   └── service-definition.json.j2      # Service registration template
└── resolved/
    └── consul.conf.j2                  # DNS forwarding configuration
```

## Configuration

Consul settings in `ansible/inventory/group_vars/all/vars.yml`:

```yaml
consul_server_address: "consul.home.goodkind.io"
consul_datacenter: home
consul_domain: int
consul_agent_enabled: true
```

Static IP fallbacks derive from `service_mapping.yml` (single source of truth for host IPs).

## Access

- **UI**: `http://consul.home.goodkind.io:8500/ui`
- **HTTP API**: `http://consul.home.goodkind.io:8500`
- **DNS**: `consul.home.goodkind.io:8600`
- **SSH**: `ssh consul@ssh.home.goodkind.io`

## Troubleshooting

```bash
# Agent not registered
ssh <service>@ssh.home.goodkind.io
systemctl status consul
journalctl -u consul --no-pager -n 50

# DNS not resolving from proxy
ssh -p 2222 root@3d06:bad:b01::110
dig @consul.home.goodkind.io -p 8600 adguard.service.int
resolvectl status

# Traefik not routing
ssh -p 2222 root@3d06:bad:b01::110
cat /etc/traefik/dynamic/routes.yml
journalctl -u traefik --no-pager -n 50
```

## Rollback

Static IPs remain as fallbacks in all Traefik/SSHPiper templates. If Consul is fully down,
set `consul_agent_enabled: false` in `group_vars/all/vars.yml` and re-run `prep-guests.yml`.
