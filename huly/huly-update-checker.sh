#!/usr/bin/env bash
# Check hcengineering/huly-selfhost GitHub releases and email once per new tag.
# Intended for a systemd timer on the Huly LXC. This script only checks for
# updates and does not perform upgrades.
set -euo pipefail

readonly env_file="${HULY_ENV_FILE:-/opt/huly/.env}"
readonly state_dir="${HULY_UPDATE_STATE_DIR:-/var/lib/huly-update-checker}"
readonly last_file="${state_dir}/last-notified"
readonly send_email="${SEND_EMAIL:-/opt/scripts/send-email}"
readonly to_email="${HULY_UPDATE_NOTIFY_TO:-alex@goodkind.io}"
readonly api_url="https://api.github.com/repos/hcengineering/huly-selfhost/releases/latest"
readonly migration_guide="https://github.com/hcengineering/huly/blob/main/MIGRATION.md"

die() {
    echo "Error: $1" >&2
    exit 1
}

if [[ ! -x "$send_email" ]]; then
    die "send-email not executable at ${send_email}"
fi

mkdir -p "$state_dir"

response="$(curl -sfS --max-time 30 \
    -H "Accept: application/vnd.github+json" \
    "$api_url")" || die "GitHub API request failed"

latest_tag="$(echo "$response" | jq -r '.tag_name')" || die "jq failed"
release_url="$(echo "$response" | jq -r '.html_url')" || die "jq failed"

if [[ -z "$latest_tag" || "$latest_tag" == "null" ]]; then
    die "missing tag_name in API response"
fi

if [[ -z "$release_url" || "$release_url" == "null" ]]; then
    release_url="https://github.com/hcengineering/huly-selfhost/releases/latest"
fi

previous="unknown"
if [[ -r "$last_file" ]]; then
    previous="$(tr -d ' \n' < "$last_file" || true)"
fi

if [[ "$previous" == "$latest_tag" ]]; then
    exit 0
fi

huly_version=""
if [[ -r "$env_file" ]]; then
    while IFS= read -r line || [[ -n "${line:-}" ]]; do
        [[ -z "${line// }" ]] && continue
        [[ "$line" =~ ^# ]] && continue
        if [[ "$line" =~ ^HULY_VERSION= ]]; then
            huly_version="${line#HULY_VERSION=}"
            huly_version="${huly_version%\"}"
            huly_version="${huly_version#\"}"
            break
        fi
    done < "$env_file"
fi

subject="Huly update available: ${previous} -> ${latest_tag}"

body="Upstream Huly published a new release.

Previous notified tag: ${previous}
Latest GitHub tag:     ${latest_tag}

HULY_VERSION in ${env_file}: ${huly_version:-not set or file missing}

Release page:
${release_url}

If this is a major/minor version bump, please review migration guidance before any
in-place upgrade:
${migration_guide}
"

if "$send_email" \
    -t "$to_email" \
    -s "$subject" \
    -m "$body" \
    -n "Huly" \
    -c "huly-update-checker"; then
    printf '%s' "$latest_tag" > "$last_file"
else
    die "send-email failed"
fi
