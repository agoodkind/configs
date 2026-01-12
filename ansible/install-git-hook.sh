#!/usr/bin/env bash
# Install this on your ansible server to auto-sync on git push
set -euo pipefail

REPO_PATH="/usr/share/configs"
HOOK_PATH="$REPO_PATH/.git/hooks/post-merge"

cat > "$HOOK_PATH" << 'EOF'
#!/usr/bin/env bash
# Auto-sync Semaphore templates after git pull/merge

# Check if ansible playbooks changed
if git diff-tree -r --name-only --no-commit-id ORIG_HEAD HEAD | grep -q "^ansible/playbooks/.*\.yml$"; then
  echo "Ansible playbooks changed, syncing Semaphore templates..."
  cd "$(git rev-parse --show-toplevel)/ansible"
  ./sync-semaphore.sh
fi
EOF

chmod +x "$HOOK_PATH"
echo "✅ Git hook installed at $HOOK_PATH"
echo "   Templates will auto-sync after 'git pull'"
