# Traefik Git Workflow

This document describes how to manage Traefik configuration using Git.

## Overview

```
┌─────────────────┐
│  Local Machine  │
│   (git push)    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  GitHub Repo    │
│  configs.git    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐      ┌──────────────────┐
│ Ansible Server  │─────▶│  Traefik Server  │
│ (pull & deploy) │      │ (configuration)  │
└─────────────────┘      └──────────────────┘
```

## Daily Workflow

### 1. Make Changes Locally

```bash
cd ~/Sites/configs/traefik

# Edit route configuration
vim dynamic/routes.yml

# Validate changes
rake validate
```

### 2. Commit and Push

```bash
git add dynamic/routes.yml
git commit -m "Add new service: myapp"
git push
```

### 3. Deploy to Production

**Option A: Automatic (via webhook/cron)**

Set up a cron job on your Ansible server:

```bash
# /etc/cron.d/traefik-sync
*/15 * * * * root cd /usr/share/configs && git pull && ansible-playbook ansible/playbooks/update-traefik-config.yml >> /var/log/traefik-sync.log 2>&1
```

**Option B: Manual**

SSH to Ansible server and run:

```bash
cd /usr/share/configs/traefik
./sync-traefik.sh
```

**Option C: From Local Machine**

```bash
cd ~/Sites/configs/ansible
ansible-playbook playbooks/update-traefik-config.yml
```

## Common Tasks

### Adding a New Public Service

1. **Edit `dynamic/routes.yml`:**

```yaml
http:
  routers:
    myapp-public:
      rule: "Host(`myapp.public.home.goodkind.io`)"
      service: myapp
      middlewares:
        - secure-headers
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt

  services:
    myapp:
      loadBalancer:
        servers:
          - url: "http://myapp.home.goodkind.io:8080"
```

2. **Validate, commit, and deploy:**

```bash
rake validate
git add dynamic/routes.yml
git commit -m "Add myapp routing"
git push

# Then deploy (see options above)
```

3. **Verify:**

```bash
curl -I https://myapp.public.home.goodkind.io
```

### Updating Middleware

```bash
vim dynamic/middlewares.yml
rake validate
git add dynamic/middlewares.yml
git commit -m "Update security headers"
git push
# Deploy as above
```

### Emergency Rollback

```bash
# On Ansible server
cd /usr/share/configs
git log --oneline  # Find commit to rollback to
git reset --hard abc123
ansible-playbook ansible/playbooks/update-traefik-config.yml
```

Or restore from backup:

```bash
systemctl stop traefik
cp -r /etc/traefik.backup /etc/traefik
systemctl start traefik
```

## Integration with Semaphore

If you use Semaphore UI, create a template for Traefik deployment:

**Playbook:** `ansible/playbooks/update-traefik-config.yml`

Then you can trigger deployments from the web UI.

## Automation Options

### Option 1: Git Hooks (Recommended)

Add a post-receive hook on your git server to trigger deployment:

```bash
#!/bin/bash
# .git/hooks/post-receive

if [[ $ref == "refs/heads/main" ]]; then
  cd /usr/share/configs
  git pull
  ansible-playbook ansible/playbooks/update-traefik-config.yml
fi
```

### Option 2: GitHub Actions

Create `.github/workflows/deploy-traefik.yml`:

```yaml
name: Deploy Traefik Config

on:
  push:
    branches: [main]
    paths:
      - 'traefik/**'

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Install Ansible
        run: pip install ansible

      - name: Deploy
        env:
          SSH_PRIVATE_KEY: ${{ secrets.SSH_PRIVATE_KEY }}
        run: |
          eval $(ssh-agent)
          echo "$SSH_PRIVATE_KEY" | ssh-add -
          ansible-playbook ansible/playbooks/update-traefik-config.yml
```

### Option 3: Webhook + Script

Use a simple webhook receiver:

```bash
# On Ansible server
curl https://example.com/webhook-endpoint
# -> Triggers: cd /usr/share/configs && git pull && ./traefik/sync-traefik.sh
```

## Best Practices

1. **Always validate before pushing:**
   ```bash
   rake validate
   ```

2. **Use descriptive commit messages:**
   ```bash
   git commit -m "Add grafana public routing with auth"
   ```

3. **Test in staging first** (if you have a staging environment)

4. **Keep secrets out of git:**
   - Use Ansible vault for sensitive data
   - Store API tokens in environment variables

5. **Review changes before deploying:**
   ```bash
   git diff HEAD~1 traefik/
   ```

6. **Monitor after deployment:**
   ```bash
   # Check Traefik logs
   ansible traefik_servers -m shell -a "journalctl -u traefik -n 50"

   # Check dashboard
   open https://traefik.public.home.goodkind.io/dashboard/
   ```

## Troubleshooting

### Changes not applying

```bash
# Force refresh on Traefik server
ansible traefik_servers -m systemd -a "name=traefik state=restarted"
```

### Configuration validation fails

```bash
cd ~/Sites/configs/traefik
rake validate  # See detailed error message
```

### Git conflicts

```bash
git pull --rebase
# Resolve conflicts
git add .
git rebase --continue
```

## Summary

The workflow is simple:

1. **Edit** configuration locally
2. **Validate** with `rake validate`
3. **Commit** and **push** to git
4. **Deploy** via Ansible (manual or automatic)
5. **Verify** the changes

This gives you:
- ✅ Version control for all changes
- ✅ Audit trail (git log)
- ✅ Easy rollback (git revert)
- ✅ Automated deployment
- ✅ Validation before deployment
