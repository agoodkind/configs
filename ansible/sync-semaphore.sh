#!/usr/bin/env bash
set -euo pipefail

SEMHOST="${SEMAPHORE_HOST:-http://ansible.home.goodkind.io:3000}"
TOKEN="${SEMAPHORE_TOKEN:-}"

PROJECT_ID=1
REPOSITORY_ID=1
INVENTORY_ID=1
ENVIRONMENT_ID=2  # Changed from 0 to 2 to match your working template

REPO_ROOT="/usr/share/configs"                # local clone of configs.git
PLAYBOOK_ROOT="$REPO_ROOT/ansible/playbooks"

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

    update_response=$(curl -s -w "\n%{http_code}" -X PUT "$SEMHOST/api/project/$PROJECT_ID/templates/$existing" \
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
)
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
  "name": "$name",
  "playbook": "$rel",
  "inventory_id": $INVENTORY_ID,
  "repository_id": $REPOSITORY_ID,
  "environment_id": $ENVIRONMENT_ID,
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
