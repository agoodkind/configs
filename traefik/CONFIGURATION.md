# Traefik Configuration Variables

This document explains how to customize Traefik for your environment.

## Overview

All Traefik configuration files are Jinja2 templates that are processed by Ansible. This allows you to:

- Change domains without editing multiple files
- Use different configurations for different environments
- Keep sensitive data secure with Ansible Vault
- Maintain consistency across all services

## Configuration Variables

Variables are defined in `ansible/inventory/group_vars/traefik_servers.yml`.

### Domain Configuration

```yaml
# Public domain for external access
traefik_public_domain: "public.home.goodkind.io"

# Internal domain for backend services
traefik_internal_domain: "home.goodkind.io"
```

**Example:** With these settings, your services will be accessible at:
- Public: `ansible.public.home.goodkind.io`
- Backend: `ansible.home.goodkind.io:3000`

### SSL/TLS Configuration

```yaml
# Email for Let's Encrypt notifications
traefik_acme_email: "admin@goodkind.io"

# Cloudflare API token for DNS-01 challenge
cloudflare_api_token: "your-api-token-here"
```

### Other Settings

```yaml
# Traefik version to install
traefik_version: "3.0.0"

# System user and group
traefik_user: traefik
traefik_group: traefik

# Directory locations
traefik_config_dir: /etc/traefik
traefik_log_dir: /var/log/traefik
repo_root: /usr/share/configs
```

## Changing Your Domain

To change your public domain (e.g., from `public.home.goodkind.io` to `external.mydomain.com`):

### 1. Update Variables

Edit `ansible/inventory/group_vars/traefik_servers.yml`:

```yaml
traefik_public_domain: "external.mydomain.com"
traefik_internal_domain: "internal.mydomain.com"  # Optional
traefik_acme_email: "letsencrypt@mydomain.com"
```

### 2. Update DNS

Create DNS records for your new domain:

```
*.external.mydomain.com.  A  <traefik-public-ip>
```

Or individual records:
```
ansible.external.mydomain.com.  A  <traefik-public-ip>
traefik.external.mydomain.com.  A  <traefik-public-ip>
```

### 3. Deploy Changes

```bash
cd /usr/share/configs/ansible
ansible-playbook playbooks/update-traefik-config.yml
```

### 4. Verify

New certificates will be automatically requested for the new domain:

```bash
curl -I https://ansible.external.mydomain.com
```

Check Traefik logs:
```bash
ansible traefik_servers -m shell -a "journalctl -u traefik -n 50"
```

## Using Ansible Vault for Secrets

### Encrypt the Cloudflare API Token

```bash
cd ansible/inventory/group_vars

# Create encrypted version
ansible-vault create traefik_servers_vault.yml
```

Add:
```yaml
---
cloudflare_api_token: "your-actual-token-here"
```

### Update the regular file

Edit `traefik_servers.yml`:

```yaml
# Reference the vaulted variable
cloudflare_api_token: "{{ vault_cloudflare_api_token }}"
```

And in `traefik_servers_vault.yml`:
```yaml
vault_cloudflare_api_token: "your-actual-token"
```

### Deploy with Vault

```bash
ansible-playbook playbooks/deploy-traefik.yml --ask-vault-pass
```

Or use a vault password file:
```bash
ansible-playbook playbooks/deploy-traefik.yml --vault-password-file ~/.vault_pass
```

## Environment-Specific Configuration

You can have different configurations for different environments:

### Directory Structure

```
ansible/inventory/
├── group_vars/
│   ├── traefik_servers.yml          # Common settings
│   ├── traefik_servers_vault.yml    # Encrypted secrets
│   └── all.yml                      # Global settings
├── host_vars/
│   ├── proxy-prod.yml               # Production overrides
│   └── proxy-staging.yml            # Staging overrides
└── hosts
```

### Example: Production Override

`ansible/inventory/host_vars/proxy-prod.yml`:

```yaml
---
traefik_public_domain: "services.mydomain.com"
traefik_acme_email: "ops@mydomain.com"
```

### Example: Staging Override

`ansible/inventory/host_vars/proxy-staging.yml`:

```yaml
---
traefik_public_domain: "staging.mydomain.com"
traefik_acme_email: "dev@mydomain.com"
```

## Adding New Services

When adding a new service, use the template variables:

**Edit `traefik/dynamic/routes.yml.j2`:**

```yaml
http:
  routers:
    myservice-public:
      rule: "Host(`myservice.{{ traefik_public_domain }}`)"
      service: myservice
      middlewares:
        - secure-headers
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt

  services:
    myservice:
      loadBalancer:
        servers:
          - url: "http://myservice.{{ traefik_internal_domain }}:8080"
```

This automatically uses your configured domains!

## Testing Configuration Changes

### Dry Run

Check what would change without applying:

```bash
ansible-playbook playbooks/update-traefik-config.yml --check --diff
```

### Validate Templates Locally

```bash
cd ansible

# Check template syntax
ansible-playbook playbooks/update-traefik-config.yml --syntax-check

# See rendered template
ansible traefik_servers -m template \
  -a "src=/usr/share/configs/traefik/dynamic/routes.yml.j2 dest=/tmp/routes-preview.yml" \
  --diff

# View the result
ansible traefik_servers -m shell -a "cat /tmp/routes-preview.yml"
```

## Rollback

If something goes wrong, revert to the previous version:

```bash
# Git rollback
cd /usr/share/configs
git log --oneline  # Find the previous commit
git checkout abc123 traefik/

# Redeploy
ansible-playbook ansible/playbooks/update-traefik-config.yml
```

## Common Scenarios

### Scenario 1: Moving to a New Domain

```bash
# 1. Update group_vars
vim ansible/inventory/group_vars/traefik_servers.yml
# Change: traefik_public_domain: "new.domain.com"

# 2. Update DNS
# Point *.new.domain.com to Traefik server

# 3. Deploy
ansible-playbook ansible/playbooks/update-traefik-config.yml

# 4. Wait for certificates (1-2 minutes)
# Monitor: journalctl -u traefik -f

# 5. Test
curl -I https://ansible.new.domain.com
```

### Scenario 2: Using Environment Variables

Instead of storing the token in Git, use environment variables:

```yaml
# In group_vars/traefik_servers.yml
cloudflare_api_token: "{{ lookup('env', 'CLOUDFLARE_API_TOKEN') }}"
```

Then deploy:
```bash
export CLOUDFLARE_API_TOKEN="your-token"
ansible-playbook playbooks/deploy-traefik.yml
```

### Scenario 3: Multiple DNS Providers

If you use different DNS providers for different domains:

```yaml
# group_vars/traefik_servers.yml
traefik_dns_provider: "{{ dns_provider | default('cloudflare') }}"
```

Update `traefik/traefik.yml.j2`:
```yaml
certificatesResolvers:
  letsencrypt:
    acme:
      dnsChallenge:
        provider: {{ traefik_dns_provider }}
```

## Best Practices

1. **Always use templates** - Don't hardcode domains
2. **Test changes** - Use `--check --diff` before deploying
3. **Use Vault for secrets** - Never commit API tokens
4. **Document overrides** - Comment why you override defaults
5. **Version control** - Commit changes to group_vars
6. **Monitor deployments** - Watch logs after changes

## Troubleshooting

### Variables not being applied

Check variable precedence:
```bash
ansible-inventory --list --host proxy.home.goodkind.io
```

### Template errors

Syntax check:
```bash
ansible-playbook playbooks/update-traefik-config.yml --syntax-check
```

### Certificate issues with new domain

Verify DNS propagation:
```bash
dig +short _acme-challenge.new.domain.com TXT
```

Check Cloudflare API token permissions:
- Zone:DNS:Edit
- Zone:Zone:Read

## Reference

### Template Variables Used

- `{{ traefik_public_domain }}` - Public-facing domain
- `{{ traefik_internal_domain }}` - Internal/backend domain
- `{{ traefik_acme_email }}` - ACME notification email
- `{{ cloudflare_api_token }}` - DNS challenge API token
- `{{ traefik_version }}` - Traefik version
- `{{ traefik_user }}` / `{{ traefik_group }}` - System user/group
- `{{ traefik_config_dir }}` - Configuration directory
- `{{ traefik_log_dir }}` - Log directory

### Files Using Templates

- `traefik/traefik.yml.j2` - Static configuration
- `traefik/dynamic/routes.yml.j2` - Routing rules
- `traefik/dynamic/middlewares.yml.j2` - Middlewares (optional)
- `ansible/templates/traefik.service.j2` - Systemd service

All of these are automatically processed when you run the playbooks!
