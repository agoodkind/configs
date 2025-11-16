# Traefik Quick Start

Get Traefik up and running in 5 minutes.

## Prerequisites

- Ansible server with configs repo cloned
- Target server for Traefik installation
- Cloudflare account with API token
- DNS access for `*.public.home.goodkind.io`

## Step 1: Update Inventory

Edit `ansible/inventory/hosts`:

```ini
[traefik_servers]
proxy.home.goodkind.io  # Your Traefik server
```

## Step 2: Configure Environment

On your Ansible server, create environment file:

```bash
# Create group_vars for traefik_servers
mkdir -p /usr/share/configs/ansible/inventory/group_vars
cat > /usr/share/configs/ansible/inventory/group_vars/traefik_servers.yml <<EOF
---
cloudflare_api_token: "your-cloudflare-api-token"
EOF

chmod 600 /usr/share/configs/ansible/inventory/group_vars/traefik_servers.yml
```

Or use Ansible Vault (more secure):

```bash
ansible-vault create ansible/inventory/group_vars/traefik_servers.yml
```

Add:
```yaml
---
cloudflare_api_token: "your-cloudflare-api-token"
```

## Step 3: Configure Domains and Email

Edit `ansible/inventory/group_vars/traefik_servers.yml`:

```yaml
# Customize these for your environment
traefik_public_domain: "public.home.goodkind.io"      # Change this!
traefik_internal_domain: "home.goodkind.io"           # Change this!
traefik_acme_email: "your-email@domain.com"          # Change this!
```

This configures all your domains in one place. SSL certificates will be automatically requested for `*.{{ traefik_public_domain }}`.

## Step 4: Configure Routes

The default routes are already configured to use your domain variables. Review `traefik/dynamic/routes.yml.j2`:

```yaml
http:
  routers:
    ansible-public:
      rule: "Host(`ansible.{{ traefik_public_domain }}`)"  # Uses your configured domain!
      service: ansible
      middlewares:
        - secure-headers
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt

  services:
    ansible:
      loadBalancer:
        servers:
          - url: "http://ansible.{{ traefik_internal_domain }}:3000"  # Uses your configured domain!
```

**Note:** Use template variables instead of hardcoding domains. This makes it easy to change domains later!

## Step 5: Set Up DNS

Create DNS records in Cloudflare:

```
*.public.home.goodkind.io.  A  <traefik-server-public-ip>
```

Or individual records:
```
ansible.public.home.goodkind.io.  A  <traefik-server-public-ip>
traefik.public.home.goodkind.io.  A  <traefik-server-public-ip>
```

## Step 6: Deploy Traefik

Initial installation:

```bash
cd /usr/share/configs/ansible
ansible-playbook playbooks/deploy-traefik.yml
```

Or using rake:

```bash
cd /usr/share/configs/traefik
rake deploy
```

## Step 7: Verify

Check Traefik is running:

```bash
ansible traefik_servers -m systemd -a "name=traefik state=status"
```

Access the dashboard:

```
https://traefik.public.home.goodkind.io/dashboard/
```

Username: `admin`
Password: `changeme123` (change this in `dynamic/middlewares.yml`!)

Test your service:

```bash
curl -I https://ansible.public.home.goodkind.io
```

## Step 8: Set Up Auto-Deploy (Optional)

Add to crontab on Ansible server:

```bash
crontab -e
```

Add:
```
*/15 * * * * cd /usr/share/configs && git pull && ansible-playbook ansible/playbooks/update-traefik-config.yml >> /var/log/traefik-sync.log 2>&1
```

## Daily Usage

### Add a new service

1. Edit `traefik/dynamic/routes.yml`
2. Validate: `rake validate`
3. Commit: `git add . && git commit -m "Add service X"`
4. Push: `git push`
5. Deploy: `cd /usr/share/configs/traefik && ./sync-traefik.sh`

### Update security settings

1. Edit `traefik/dynamic/middlewares.yml`
2. Follow same process as above

### Change dashboard password

```bash
# Generate new password
htpasswd -nb admin yournewpassword

# Update dynamic/middlewares.yml with the output
# Commit and deploy
```

## Troubleshooting

### Certificates not issuing

Check logs:
```bash
ansible traefik_servers -m shell -a "journalctl -u traefik | grep acme"
```

Verify Cloudflare API token has DNS edit permissions.

### Service not routing

1. Check service is reachable:
   ```bash
   curl http://ansible.home.goodkind.io:3000
   ```

2. Check Traefik config:
   ```bash
   ansible traefik_servers -m shell -a "cat /etc/traefik/dynamic/routes.yml"
   ```

3. Restart Traefik:
   ```bash
   ansible traefik_servers -m systemd -a "name=traefik state=restarted"
   ```

### Dashboard shows 404

Make sure the router is defined and dashboard is enabled in `traefik.yml`.

## Next Steps

- Read [WORKFLOW.md](WORKFLOW.md) for detailed Git workflow
- Read [README.md](README.md) for comprehensive documentation
- Set up monitoring and alerts
- Add more services to your reverse proxy

## Security Checklist

- [ ] Changed dashboard password from default
- [ ] Configured IP whitelist for sensitive services
- [ ] Set up rate limiting
- [ ] Enabled security headers
- [ ] Restricted Traefik API access
- [ ] Set proper file permissions (600 for secrets)
- [ ] Using Ansible Vault for API tokens
- [ ] Enabled HTTPS redirect
- [ ] Configured HSTS headers

## Support

For issues or questions:
- Check [README.md](README.md) troubleshooting section
- Review Traefik logs: `journalctl -u traefik -f`
- Check Traefik docs: https://doc.traefik.io/
