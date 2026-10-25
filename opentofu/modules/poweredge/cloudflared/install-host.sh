#!/usr/bin/env bash
set -euo pipefail

STAGING_UNIT=""
PACKAGE_FILE=""

cleanup() {
    systemctl stop "$STAGING_UNIT"
    if [[ -n "$PACKAGE_FILE" ]]; then
        rm -- "$PACKAGE_FILE"
    fi
}

main() {
    local package_url="${1:?Supply the cloudflared package URL}"
    local package_sha256="${2:?Supply the cloudflared package SHA-256}"
    STAGING_UNIT="${3:?Supply the systemd staging unit}"
    local staging_directory="/run/${STAGING_UNIT%.service}"
    trap cleanup EXIT
    PACKAGE_FILE="$(mktemp /var/tmp/cloudflared.XXXXXXXX.deb)"

    curl --fail --silent --show-error --location --proto '=https' --output "$PACKAGE_FILE" "$package_url"
    local actual_digest
    actual_digest="$(sha256sum "$PACKAGE_FILE")"
    if [[ "${actual_digest%% *}" != "$package_sha256" ]]; then
        printf '%s\n' 'The cloudflared package does not match the pinned SHA-256.' >&2
        exit 1
    fi
    dpkg --install "$PACKAGE_FILE"
    install -D -m 0600 "$staging_directory/token" /etc/cloudflared/token
    systemd-analyze verify /etc/systemd/system/cloudflared.service
    systemctl daemon-reload
    systemctl enable cloudflared.service
    systemctl restart cloudflared.service
    systemctl is-active cloudflared.service
    trap - EXIT
    cleanup
}

main "$@"
