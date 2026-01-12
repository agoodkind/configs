#!/usr/bin/env bash
set -euo pipefail

SEMHOST="${SEMAPHORE_HOST:-http://ansible.home.goodkind.io:3000}"
TOKEN="${SEMAPHORE_TOKEN:-}"

PROJECT_ID=1
REPOSITORY_ID=1
INVENTORY_ID=1
DEFAULT_ENVIRONMENT_ID=2  # Used only when creating new templates

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
PLAYBOOK_ROOT="${PLAYBOOK_ROOT:-$SCRIPT_DIR/playbooks}"

if [ ! -d "$PLAYBOOK_ROOT" ]; then
  echo "❌ Playbook directory not found: $PLAYBOOK_ROOT"
  exit 1
fi

# Check authentication
echo "=== Checking authentication ==="
if [ -z "$TOKEN" ]; then
  echo "❌ Error: SEMAPHORE_TOKEN is not set"
  echo "Please set the SEMAPHORE_TOKEN environment variable"
  exit 1
fi

auth_response=$(curl -s -w "\n%{http_code}" "$SEMHOST/api/project/$PROJECT_ID" \
  -H "Authorization: Bearer $TOKEN")
http_code=$(echo "$auth_response" | tail -n1)

if [ "$http_code" != "200" ]; then
  echo "❌ Authentication failed (HTTP $http_code)"
  echo "Please check your SEMAPHORE_TOKEN and SEMAPHORE_HOST"
  exit 1
fi

echo "✓ Authentication successful"
echo

# Build list of valid playbook names
declare -a valid_playbooks=()
for f in "$PLAYBOOK_ROOT"/*.yml; do
  [ -e "$f" ] || continue
  name="$(basename "${f%.yml}")"

  valid_playbooks+=("$name")
done

echo "=== Syncing templates ==="
echo

# Create or update templates for existing playbooks
for f in "$PLAYBOOK_ROOT"/*.yml; do
  [ -e "$f" ] || continue

  name="$(basename "${f%.yml}")"           # e.g. site

  rel="${f#"$REPO_ROOT"/}"                    # e.g. ansible/playbooks/site.yml

  echo "Checking if template '$name' exists..."

  # Check if template already exists
  existing_template=$(curl -s "$SEMHOST/api/project/$PROJECT_ID/templates" \
    -H "Authorization: Bearer $TOKEN" \
    | jq -r ".[] | select(.name == \"$name\")")

  existing=$(echo "$existing_template" | jq -r '.id // empty')

  if [ -n "$existing" ]; then
    echo "  Template '$name' already exists (ID: $existing), updating..."

    # Preserve ALL existing fields and only update specific ones
    echo "  Preserving all existing settings (surveys, CLI args, etc.)"

    # Build update payload by merging existing template with our updates
    update_payload=$(echo "$existing_template" | jq -c \
      --arg name "$name" \
      --arg playbook "$rel" \
      --argjson inventory_id "$INVENTORY_ID" \
      --argjson repository_id "$REPOSITORY_ID" \
      '. + {
        "name": $name,
        "playbook": $playbook,
        "inventory_id": $inventory_id,
        "repository_id": $repository_id
      }')

    update_response=$(curl -s -w "\n%{http_code}" -X PUT "$SEMHOST/api/project/$PROJECT_ID/templates/$existing" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "$update_payload")
    update_http_code=$(echo "$update_response" | tail -n1)
    update_body=$(echo "$update_response" | sed '$d')

    if [ "$update_http_code" == "204" ] || [ "$update_http_code" == "200" ]; then
      echo "  ✓ Updated template '$name'"
    else
      echo "  ❌ Failed to update template '$name' (HTTP $update_http_code)"
      echo "  Response: $update_body"
    fi
  else
    echo "  Creating new template '$name'..."

    create_response=$(curl -s -w "\n%{http_code}" -X POST "$SEMHOST/api/project/$PROJECT_ID/templates" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d @- <<EOF
{
  "project_id": $PROJECT_ID,
  "name": "$name",
  "playbook": "$rel",
  "inventory_id": $INVENTORY_ID,
  "repository_id": $REPOSITORY_ID,
  "environment_id": $DEFAULT_ENVIRONMENT_ID,
  "app": "ansible"
}
EOF
)
    create_http_code=$(echo "$create_response" | tail -n1)
    create_body=$(echo "$create_response" | sed '$d')

    if [ "$create_http_code" == "201" ] || [ "$create_http_code" == "200" ]; then
      created_id=$(echo "$create_body" | jq -r '.id // empty')
      if [ -n "$created_id" ]; then
        echo "  ✓ Created template '$name' (ID: $created_id)"
      else
        echo "  ✓ Created template '$name'"
      fi
    else
      echo "  ❌ Failed to create template '$name' (HTTP $create_http_code)"
      echo "  Response: $create_body"
    fi
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
    delete_response=$(curl -s -w "\n%{http_code}" -X DELETE "$SEMHOST/api/project/$PROJECT_ID/templates/$template_id" \
      -H "Authorization: Bearer $TOKEN")
    delete_http_code=$(echo "$delete_response" | tail -n1)

    if [ "$delete_http_code" == "204" ] || [ "$delete_http_code" == "200" ]; then
      echo "  ✗ Deleted template '$template_name'"
    else
      echo "  ❌ Failed to delete template '$template_name' (HTTP $delete_http_code)"
    fi
  fi
done

echo
echo "✅ Sync complete!"
