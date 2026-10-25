#!/usr/bin/env bash
set -euo pipefail

cleanup() {
    local staging_unit="$1"
    local package_file="$2"
    systemctl stop "$staging_unit"
    if [[ -n "$package_file" ]]; then
        rm -- "$package_file"
    fi
}

main() {
    local package_url="${1:?Supply the cloudflared package URL}"
    local package_sha256="${2:?Supply the cloudflared package SHA-256}"
    local staging_unit="${3:?Supply the systemd staging unit}"
    local staging_directory="/run/${staging_unit%.service}"
    local package_file=""
    trap 'cleanup "$staging_unit" "$package_file"' EXIT
    package_file="$(mktemp /var/tmp/cloudflared.XXXXXXXX.deb)"

    curl --fail --silent --show-error --location --proto '=https' --output "$package_file" "$package_url"
    local actual_digest
    actual_digest="$(sha256sum "$package_file")"
    if [[ "${actual_digest%% *}" != "$package_sha256" ]]; then
        printf '%s\n' 'The cloudflared package does not match the pinned SHA-256.' >&2
        exit 1
    fi
    dpkg --install "$package_file"
    install -D -m 0600 "$staging_directory/token" /etc/cloudflared/token
    systemd-analyze verify /etc/systemd/system/cloudflared.service
    systemctl daemon-reload
    systemctl enable cloudflared.service
    systemctl restart cloudflared.service
    systemctl is-active cloudflared.service
    trap - EXIT
    cleanup "$staging_unit" "$package_file"
}

main "$@"
