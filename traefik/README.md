# Traefik Configuration

Reverse proxy and load balancer configuration for `*.public.home.goodkind.io` services.

## Structure

```
traefik/
├── traefik.yml.j2           # Static configuration (templated)
├── dynamic/
│   ├── routes.yml.j2        # Service routing rules (templated)
│   ├── middlewares.yml.j2   # Security headers, auth, rate limiting (templated)
│   └── tls.yml             # TLS options (optional)
├── CONFIGURATION.md         # Domain and variable configuration guide
├── QUICKSTART.md            # Quick setup guide
├── WORKFLOW.md              # Git workflow documentation
├── docker-compose.yml       # Docker deployment (optional)
└── Rakefile                 # Validation and deployment tasks
```

**Note:** Configuration files use Jinja2 templating, allowing you to easily change domains and settings. See [CONFIGURATION.md](CONFIGURATION.md) for details.

## Usage

### Validate Configuration

```bash
cd traefik
rake validate
```

### Deploy Traefik (Initial Setup)

```bash
# Full installation
rake deploy

# Or manually:
cd ../ansible
ansible-playbook playbooks/deploy-traefik.yml
```

### Update Configuration Only

After making changes to routing rules or middlewares:

```bash
rake update

# Or manually:
ansible-playbook ../ansible/playbooks/update-traefik-config.yml
```

## Configuring Domains

All domains are configurable via Ansible variables. Edit `ansible/inventory/group_vars/traefik_servers.yml`:

```yaml
# Change these to match your setup
traefik_public_domain: "public.home.goodkind.io"
traefik_internal_domain: "home.goodkind.io"
traefik_acme_email: "admin@goodkind.io"
```

After changing domains, redeploy:

```bash
ansible-playbook ../ansible/playbooks/update-traefik-config.yml
```

Certificates will be automatically requested for the new domain. See [CONFIGURATION.md](CONFIGURATION.md) for detailed instructions.

## Adding a New Service

1. **Edit `dynamic/routes.yml.j2`:**

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

**Note:** Use template variables `{{ traefik_public_domain }}` and `{{ traefik_internal_domain }}` instead of hardcoding domains.

2. **Validate and deploy:**

```bash
rake validate
git add dynamic/routes.yml
git commit -m "Add myservice routing"
git push
rake update
```

## DNS Setup

Create DNS records for public services:

```
myservice.public.home.goodkind.io.  A  <traefik-public-ip>
```

Or use a wildcard:

```
*.public.home.goodkind.io.  A  <traefik-public-ip>
```

## Authentication

The Traefik dashboard is protected with basic auth. Default credentials:
- Username: `admin`
- Password: `changeme123`

To generate new credentials:

```bash
htpasswd -nb username password
```

Update `dynamic/middlewares.yml` with the output.

## Automatic Deployment

Configuration updates are automatically deployed when changes are pushed to the `configs` repo.

## Certificates

Traefik automatically obtains and renews Let's Encrypt certificates using DNS-01 challenge via Cloudflare.

Certificates are stored in `/etc/traefik/acme.json` on the Traefik server.

## Monitoring

Check Traefik status:

```bash
rake status
```

View logs:

```bash
ansible traefik_servers -m shell -a "journalctl -u traefik -f"
```

Access dashboard:

```
https://traefik.public.home.goodkind.io/dashboard/
```

## Troubleshooting

### Configuration not updating

Dynamic configuration is watched and auto-reloaded. Static config requires restart:

```bash
ansible traefik_servers -m systemd -a "name=traefik state=restarted"
```

### Certificate issues

Check ACME logs:

```bash
ansible traefik_servers -m shell -a "journalctl -u traefik | grep acme"
```

Verify DNS challenge:

```bash
dig +short _acme-challenge.public.home.goodkind.io TXT
```

### Service not routing

1. Verify the service is reachable from Traefik server
2. Check logs for errors
3. Verify DNS resolution
4. Test health check endpoint
