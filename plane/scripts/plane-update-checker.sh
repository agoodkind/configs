#!/usr/bin/env bash
# Check makeplane/plane GitHub releases; email once per new tag via send-email.
# Intended for systemd timer on the Plane LXC. Does not upgrade containers.
set -euo pipefail

readonly env_file="${PLANE_ENV_FILE:-/opt/plane/.env}"
readonly state_dir="${PLANE_UPDATE_STATE_DIR:-/var/lib/plane-update-checker}"
readonly last_file="${state_dir}/last-notified"
readonly send_email="${SEND_EMAIL:-/opt/scripts/send-email}"
readonly to_email="${PLANE_UPDATE_NOTIFY_TO:-alex@goodkind.io}"
readonly api_url="https://api.github.com/repos/makeplane/plane/releases/latest"

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

previous="unknown"
if [[ -r "$last_file" ]]; then
    previous="$(tr -d ' \n' < "$last_file" || true)"
fi

if [[ "$previous" == "$latest_tag" ]]; then
    exit 0
fi

app_release=""
if [[ -r "$env_file" ]]; then
    while IFS= read -r line || [[ -n "${line:-}" ]]; do
        [[ -z "${line// }" ]] && continue
        [[ "$line" =~ ^# ]] && continue
        if [[ "$line" =~ ^APP_RELEASE= ]]; then
            app_release="${line#APP_RELEASE=}"
            app_release="${app_release//\"/}"
            break
        fi
    done < "$env_file"
fi

subject="Plane update available: ${previous} -> ${latest_tag}"

body="Upstream Plane published a new release.

Previous notified tag: ${previous}
Latest GitHub tag:     ${latest_tag}

APP_RELEASE in ${env_file}: ${app_release:-not set or file missing}

Release page:
${release_url}

Upgrade (Community): follow Plane docs. Download latest setup.sh, choose Upgrade,
merge variables-upgrade.env into your plane.env, then start services.
https://developers.plane.so/self-hosting/manage/upgrade-plane
"

if "$send_email" \
    -t "$to_email" \
    -s "$subject" \
    -m "$body" \
    -n "Plane" \
    -c "plane-update-checker"; then
    printf '%s' "$latest_tag" > "$last_file"
else
    die "send-email failed"
fi
