# Consul Service Discovery

This directory contains Consul service discovery implementation for the infrastructure.

## Quick Start

```bash
# 1. Deploy Consul server
ansible-playbook playbooks/deploy-consul.yml

# 2. Install agents on existing containers
ansible-playbook playbooks/prep-guests.yml -e "target_hosts=all"

# 3. Deploy external host agents
ansible-playbook playbooks/deploy-consul-external.yml

# 4. Redeploy services to register with Consul
ansible-playbook playbooks/deploy-adguard.yml
ansible-playbook playbooks/deploy-proxy.yml
```

## Documentation

- **[DEPLOYMENT.md](DEPLOYMENT.md)** - Step-by-step deployment guide
- **[IMPLEMENTATION_SUMMARY.md](IMPLEMENTATION_SUMMARY.md)** - Complete implementation details

## Directory Structure

```
consul/
├── README.md                           # This file
├── DEPLOYMENT.md                       # Deployment guide
├── IMPLEMENTATION_SUMMARY.md           # Implementation details
├── consul-server.hcl.j2                # Consul server configuration
├── consul-agent.hcl.j2                 # Consul agent configuration
├── consul.service                      # systemd service unit
├── services/
│   └── service-definition.json.j2      # Service registration template
└── resolved/
    └── consul.conf.j2                  # DNS forwarding configuration
```

## Key Concepts

### Service Discovery Pattern

1. **Deploy Time**: Ansible queries Consul HTTP API for service IPs
2. **Runtime**: Services use DNS (`*.service.int`) for dynamic resolution
3. **Fallback**: Static IPs ensure availability if Consul is down

### Automatic Agent Deployment

- All new containers automatically get Consul agents via `prep-guests.yml`
- No manual agent installation required
- Disable per-host: `consul_agent_enabled: false`

### Service Registration

Each service deployment:

1. Deploys service-specific configuration
2. Registers with Consul including health checks
3. Agent reloads and updates server

## Configuration

See `ansible/inventory/group_vars/all/vars.yml`:

```yaml
consul_server_address: "consul.home.goodkind.io"
consul_datacenter: home
consul_domain: int
consul_agent_enabled: true
```

## Access

- **UI**: `http://consul.home.goodkind.io:8500/ui`
- **HTTP API**: `http://consul.home.goodkind.io:8500`
- **DNS**: `consul.home.goodkind.io:8600`
- **SSH**: `ssh consul@ssh.home.goodkind.io`

## Verification

```bash
# Check service catalog
ssh consul@ssh.home.goodkind.io
consul catalog services
consul catalog nodes -service=adguard

# Test DNS resolution
dig @consul.home.goodkind.io -p 8600 adguard.service.int
```
