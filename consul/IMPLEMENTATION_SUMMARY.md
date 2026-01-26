# Consul Service Discovery Implementation Summary

## Completed Tasks

All 10 phases of the Consul service discovery implementation have been completed:

### ✅ Phase 1: Consul Server LXC Container

- Created `consul/consul-server.hcl.j2` - Consul server configuration
- Created `consul/consul.service` - systemd unit for Consul
- Created `ansible/inventory/group_vars/consul_servers.yml` - Consul server variables
- Created `ansible/playbooks/deploy-consul.yml` - Consul server deployment playbook
- Added `consul_servers` group to `ansible/inventory/hosts`

### ✅ Phase 2: Consul Agent Deployment

- Created `consul/consul-agent.hcl.j2` - Consul agent configuration template
- Created `ansible/playbooks/tasks/install-consul-agent.yml` - Reusable agent installation task
- Updated `ansible/inventory/group_vars/all/vars.yml` - Added Consul service discovery variables

### ✅ Phase 3: Integration with prep-guests.yml

- Updated `ansible/playbooks/prep-guests.yml` - Added Consul agent installation task
- Agents automatically deployed to all new containers
- Opt-out via `consul_agent_enabled: false`

### ✅ Phase 4: Service Definitions

- Created `consul/services/service-definition.json.j2` - Generic service definition template
- Updated `ansible/playbooks/deploy-adguard.yml` - Added AdGuard service registration
- Updated `ansible/playbooks/deploy-mwan.yml` - Added MWAN service registration
- Updated `ansible/playbooks/deploy-proxy.yml` - Added proxy service registration

### ✅ Phase 5: External Host Support

- Created `ansible/playbooks/deploy-consul-external.yml` - Deploys agents to Proxmox, NAS, mini, OPNsense

### ✅ Phase 6: Ansible HTTP API Integration

- Created `ansible/playbooks/tasks/resolve-consul-services.yml` - Queries Consul HTTP API at deploy time
- Builds `consul_resolved_ips` fact map for template use
- Fallback to static IPs when Consul unavailable

### ✅ Phase 7: Traefik Configuration Update

- Updated `traefik/dynamic/routes.yml.j2` - Uses DNS primary + resolved fallback pattern
- Example: `adguard.service.int` with fallback to `[3d06:bad:b01::53]`
- Added health checks to services

### ✅ Phase 8: SSHPiper Configuration Update

- Updated `sshpiper/sshpiperd.yaml.j2` - Uses Consul-resolved IPs when available
- Falls back to `ansible_host` from inventory

### ✅ Phase 9: DNS Forwarding Configuration

- Created `consul/resolved/consul.conf.j2` - systemd-resolved config for `.int` domain
- Forwards `.int` queries to Consul DNS on port 8600

### ✅ Phase 10: Integration with deploy-proxy.yml

- Updated `ansible/playbooks/deploy-proxy.yml`:
  - Resolves service IPs from Consul before templating
  - Configures DNS forwarding for runtime resolution
  - Added systemd-resolved restart handler

## Files Created

### Configuration Templates

- `/consul/consul-server.hcl.j2` - Consul server config
- `/consul/consul-agent.hcl.j2` - Consul agent config
- `/consul/consul.service` - systemd service unit
- `/consul/services/service-definition.json.j2` - Service registration template
- `/consul/resolved/consul.conf.j2` - DNS forwarding config

### Ansible Playbooks & Tasks

- `/ansible/playbooks/deploy-consul.yml` - Consul server deployment
- `/ansible/playbooks/deploy-consul-external.yml` - External host agent deployment
- `/ansible/playbooks/tasks/install-consul-agent.yml` - Reusable agent installation
- `/ansible/playbooks/tasks/resolve-consul-services.yml` - HTTP API query task

### Documentation

- `/consul/DEPLOYMENT.md` - Step-by-step deployment guide
- `/consul/IMPLEMENTATION_SUMMARY.md` - This file

### Inventory & Variables

- `/ansible/inventory/group_vars/consul_servers.yml` - Consul server configuration
- Updated `/ansible/inventory/group_vars/all/vars.yml` - Added Consul variables
- Updated `/ansible/inventory/hosts` - Added consul_servers group

## Files Modified

### Ansible Playbooks

- `/ansible/playbooks/prep-guests.yml` - Added Consul agent installation
- `/ansible/playbooks/deploy-adguard.yml` - Added service registration + Reload Consul handler
- `/ansible/playbooks/deploy-mwan.yml` - Added service registration + Reload Consul handler
- `/ansible/playbooks/deploy-proxy.yml` - Added Consul resolution + DNS forwarding + handlers

### Configuration Templates

- `/traefik/dynamic/routes.yml.j2` - Updated to use Consul DNS with fallbacks
- `/sshpiper/sshpiperd.yaml.j2` - Updated to use Consul-resolved IPs

## Key Features

### Automatic Agent Deployment

- Every container created via `prep-guests.yml` automatically gets a Consul agent
- No manual agent installation required
- Can disable per-host with `consul_agent_enabled: false`

### Dual Resolution Strategy

- **Deploy Time**: Ansible queries HTTP API, builds `consul_resolved_ips` fact
- **Runtime**: Traefik/SSHPiper use DNS queries to `*.service.int`
- **Fallback**: Static IPs in `consul_static_ips` if Consul unavailable

### Health Checking

- Each service registers health checks with Consul
- Only healthy services returned by Consul queries
- Traefik health checks provide additional redundancy

### Service-Specific Registration

- Each deploy playbook registers its own service definition
- Custom health checks per service type
- Tags for categorization (dns, lxc, gateway, vm, etc.)

## Configuration Reference

### Consul Server

- **Hostname**: `consul.home.goodkind.io`
- **IPv6**: `3d06:bad:b01::106/64`
- **MAC**: `bc:24:11:ee:85:00`
- **Datacenter**: `home`
- **Domain**: `int`
- **UI**: Enabled on port 8500
- **DNS**: Port 8600
- **HTTP API**: Port 8500

### Service Registration Format

```json
{
  "service": {
    "name": "adguard",
    "tags": ["dns", "lxc"],
    "address": "3d06:bad:b01::53",
    "port": 80,
    "checks": [
      {
        "http": "http://localhost:80/",
        "interval": "30s",
        "timeout": "5s"
      }
    ]
  }
}
```

### DNS Resolution

- Services accessible as: `<service>.service.int`
- Example: `adguard.service.int` resolves to `3d06:bad:b01::53`
- Queries forwarded by systemd-resolved to Consul DNS

### Static IP Fallbacks

Static IPs are derived from the single source of truth in
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

## Next Steps

1. **Deploy Consul Server**: `ansible-playbook playbooks/deploy-consul.yml`
2. **Install Agents**: Re-run `prep-guests.yml` on existing containers
3. **Register Services**: Redeploy services to register with Consul
4. **Verify**: Check `consul catalog services` and DNS resolution
5. **Test**: Ensure Traefik routes using Consul DNS

## Future Enhancements

- **ACLs**: Enable Consul ACLs for security
- **Connect**: Add Consul Connect for mTLS between services
- **KV Store**: Use Consul KV for dynamic configuration
- **Watches**: Implement Consul watches for automatic config updates
