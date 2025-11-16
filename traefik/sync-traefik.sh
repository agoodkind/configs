#!/usr/bin/env bash
set -euo pipefail

# Sync Traefik configuration from git to production
# Run this after pushing changes to the configs repo

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== Syncing Traefik Configuration ==="
echo

# Pull latest changes
echo "📥 Pulling latest changes from git..."
git pull --quiet
echo "✓ Git pull successful"
echo

# Validate configuration
echo "🔍 Validating configuration..."
cd traefik
if command -v rake &> /dev/null; then
  rake validate
else
  echo "⚠️  Rake not found, skipping validation"
fi
echo

# Deploy configuration update
echo "🚀 Deploying configuration to Traefik servers..."
cd "$REPO_ROOT/ansible"
ansible-playbook playbooks/update-traefik-config.yml

echo
echo "✅ Traefik configuration sync complete!"
