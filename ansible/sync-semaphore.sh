#!/usr/bin/env bash
set -euo pipefail

SEMHOST="http://ansible.home.goodkind.io:3000"
TOKEN="gun8h1ugmrepllnio2qlcypjcomac2vxsmfrfwbvzue="

PROJECT_ID=1
REPOSITORY_ID=1
INVENTORY_ID=1
ENVIRONMENT_ID=2  # Changed from 0 to 2 to match your working template

REPO_ROOT="/usr/share/configs"                # local clone of configs.git
PLAYBOOK_ROOT="$REPO_ROOT/ansible/playbooks"

# Skip debug/test playbooks
SKIP_PATTERNS="debug|dump|test"

# Build list of valid playbook names
declare -a valid_playbooks=()
for f in "$PLAYBOOK_ROOT"/*.yml; do
  [ -e "$f" ] || continue
  name="$(basename "${f%.yml}")"
  
  # Skip debug/test playbooks
  if echo "$name" | grep -qE "$SKIP_PATTERNS"; then
    continue
  fi
  
  valid_playbooks+=("$name")
done

echo "=== Syncing templates ==="
echo

# Create or update templates for existing playbooks
for f in "$PLAYBOOK_ROOT"/*.yml; do
  [ -e "$f" ] || continue
  
  name="$(basename "${f%.yml}")"           # e.g. site
  
  # Skip debug/test playbooks
  if echo "$name" | grep -qE "$SKIP_PATTERNS"; then
    echo "Skipping $name (matches skip pattern)"
    continue
  fi
  
  rel="${f#"$REPO_ROOT"/}"                    # e.g. ansible/playbooks/site.yml

  echo "Checking if template '$name' exists..."
  
  # Check if template already exists
  existing=$(curl -s "$SEMHOST/api/project/$PROJECT_ID/templates" \
    -H "Authorization: Bearer $TOKEN" \
    | jq -r ".[] | select(.name == \"$name\") | .id")
  
  if [ -n "$existing" ]; then
    echo "  Template '$name' already exists (ID: $existing), updating..."
    
    curl -s -X PUT "$SEMHOST/api/project/$PROJECT_ID/templates/$existing" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d @- <<EOF
{
  "id": $existing,
  "name": "$name",
  "playbook": "$rel",
  "inventory_id": $INVENTORY_ID,
  "repository_id": $REPOSITORY_ID,
  "environment_id": $ENVIRONMENT_ID,
  "app": "ansible"
}
EOF
    echo "  ✓ Updated template '$name'"
  else
    echo "  Creating new template '$name'..."
    
    curl -s -X POST "$SEMHOST/api/project/$PROJECT_ID/templates" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d @- <<EOF
{
  "name": "$name",
  "playbook": "$rel",
  "inventory_id": $INVENTORY_ID,
  "repository_id": $REPOSITORY_ID,
  "environment_id": $ENVIRONMENT_ID,
  "app": "ansible"
}
EOF
    echo "  ✓ Created template '$name'"
  fi
  
  echo
done

echo
echo "=== Cleaning up orphaned templates ==="
echo

# Get all templates from Semaphore
all_templates=$(curl -s "$SEMHOST/api/project/$PROJECT_ID/templates" \
  -H "Authorization: Bearer $TOKEN")

# Check each template in Semaphore
echo "$all_templates" | jq -c '.[]' | while read -r template; do
  template_id=$(echo "$template" | jq -r '.id')
  template_name=$(echo "$template" | jq -r '.name')
  
  # Check if this template name exists in our valid playbooks
  found=false
  for valid_name in "${valid_playbooks[@]}"; do
    if [ "$template_name" == "$valid_name" ]; then
      found=true
      break
    fi
  done
  
  if [ "$found" == "false" ]; then
    echo "  Removing orphaned template '$template_name' (ID: $template_id)..."
    curl -s -X DELETE "$SEMHOST/api/project/$PROJECT_ID/templates/$template_id" \
      -H "Authorization: Bearer $TOKEN" > /dev/null
    echo "  ✗ Deleted template '$template_name'"
  fi
done

echo
echo "✅ Sync complete!"
