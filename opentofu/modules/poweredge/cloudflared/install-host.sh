#!/usr/bin/env bash
set -euo pipefail

main() {
    local package_url="${1:?Supply the cloudflared package URL}"
    local package_sha256="${2:?Supply the cloudflared package SHA-256}"
    local package_file
    package_file="$(mktemp /var/tmp/cloudflared.XXXXXXXX.deb)"
    trap 'rm -- "$package_file"' EXIT

    curl --fail --silent --show-error --location --proto '=https' --output "$package_file" "$package_url"
    local actual_digest
    actual_digest="$(sha256sum "$package_file")"
    if [[ "${actual_digest%% *}" != "$package_sha256" ]]; then
        printf '%s\n' 'The cloudflared package does not match the pinned SHA-256.' >&2
        exit 1
    fi
    dpkg --install "$package_file"
    install -D -m 0600 /run/poweredge-cloudflared-token /etc/cloudflared/token
    rm -- /run/poweredge-cloudflared-token
    systemd-analyze verify /etc/systemd/system/cloudflared.service
    systemctl daemon-reload
    systemctl enable cloudflared.service
    systemctl restart cloudflared.service
    systemctl is-active cloudflared.service
    rm -- "$package_file"
    trap - EXIT
}

main "$@"
