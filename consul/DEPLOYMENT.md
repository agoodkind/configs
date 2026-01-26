# Consul Service Discovery Deployment Guide

## Overview

This implementation deploys Consul as the service discovery backbone for your infrastructure. Consul agents on each LXC container handle automatic registration and health checking, while Ansible queries Consul's HTTP API at deploy time to generate Traefik/SSHPiper configs with DNS primary and resolved IP fallbacks.

## Architecture

- **Consul Server**: Single LXC container (`consul.home.goodkind.io` - `3d06:bad:b01::106`)
- **Consul Agents**: Deployed to all LXC containers via `prep-guests.yml`
- **Service Discovery**: HTTP API queries at deploy time + DNS runtime resolution
- **Fallback**: Static IPs derived from `inventory/group_vars/all/service_mapping.yml`

## Deployment Order

### 1. Deploy Consul Server Container

```bash
cd /Users/agoodkind/Sites/configs/ansible
ansible-playbook playbooks/deploy-consul.yml
```

This creates the Consul server LXC container at `3d06:bad:b01::106`.

### 2. Verify Consul Server

```bash
ssh consul@ssh.home.goodkind.io
consul members
consul catalog services
```

### 3. Deploy Consul Agents to Existing Containers

Re-run `prep-guests.yml` on existing containers to install Consul agents:

```bash
ansible-playbook playbooks/prep-guests.yml -e "target_hosts=adguard_servers"
ansible-playbook playbooks/prep-guests.yml -e "target_hosts=mwan_servers"
ansible-playbook playbooks/prep-guests.yml -e "target_hosts=ansible_servers"
ansible-playbook playbooks/prep-guests.yml -e "target_hosts=proxy_servers"
```

### 4. Deploy Service Definitions

Redeploy each service to register with Consul:

```bash
ansible-playbook playbooks/deploy-adguard.yml
ansible-playbook playbooks/deploy-mwan.yml
ansible-playbook playbooks/deploy-proxy.yml
```

### 5. Deploy Consul Agents to External Hosts

```bash
ansible-playbook playbooks/deploy-consul-external.yml
```

This deploys Consul agents to Proxmox, NAS, mini, and OPNsense.

### 6. Verify Service Registration

```bash
ssh consul@ssh.home.goodkind.io
consul catalog services
consul catalog nodes -service=adguard
consul catalog nodes -service=mwan
```

### 7. Test DNS Resolution (from proxy container)

```bash
ssh -p 2222 root@3d06:bad:b01::110
dig @consul.home.goodkind.io -p 8600 adguard.service.int
dig @consul.home.goodkind.io -p 8600 mwan.service.int
```

### 8. Verify Traefik Uses Consul DNS

```bash
ssh -p 2222 root@3d06:bad:b01::110
cat /etc/traefik/dynamic/routes.yml
# Should show DNS names like adguard.service.int with fallback IPs
```

## Configuration Variables

Consul settings are in `inventory/group_vars/all/vars.yml`:

```yaml
consul_server_address: "consul.home.goodkind.io"
consul_datacenter: home
consul_domain: int
consul_agent_enabled: true
```

Static IP fallbacks are derived from the single source of truth in
`inventory/group_vars/all/service_mapping.yml`:

```yaml
service_mapping:
  adguard:
    hostname: adguard.home.goodkind.io
    ipv6: "3d06:bad:b01::53"
  consul:
    hostname: consul.home.goodkind.io
    ipv6: "3d06:bad:b01::106"
  # ... other services
```

The `consul_static_ips` variable is automatically derived from `service_mapping`.

## How It Works

### Deploy Time (Ansible)

1. `tasks/resolve-consul-services.yml` queries Consul HTTP API
2. Builds `consul_resolved_ips` fact map
3. Templates use `consul_resolved_ips` as fallback in Traefik/SSHPiper configs

### Runtime (Services)

1. Traefik tries `adguard.service.int` (DNS via systemd-resolved -> Consul)
2. Falls back to static IP if DNS fails
3. Health checks ensure only healthy instances are used

### Service Registration

Each `deploy-*.yml` playbook:

1. Deploys service configuration
2. Registers service with Consul using `/etc/consul.d/<service>.json`
3. Consul agent reloads and registers with server

## Rollback Strategy

- Static IPs remain as fallbacks in all configs
- If Consul is down: Ansible uses static IPs, Traefik uses fallback servers
- Disable Consul: Set `consul_agent_enabled: false` in group_vars

## Future Enhancements

- Enable Consul ACLs for security
- Add Consul Connect for mTLS between services
- Consul KV for dynamic configuration storage
- Consul Watches for automatic config regeneration

## Troubleshooting

### Service not registered

```bash
ssh <service>@ssh.home.goodkind.io
systemctl status consul
journalctl -u consul -f
consul catalog services
```

### DNS not resolving

```bash
ssh -p 2222 root@3d06:bad:b01::110
systemctl status systemd-resolved
resolvectl status
dig @consul.home.goodkind.io -p 8600 adguard.service.int
```

### Traefik not routing

```bash
ssh -p 2222 root@3d06:bad:b01::110
systemctl status traefik
journalctl -u traefik -f
cat /etc/traefik/dynamic/routes.yml
```
