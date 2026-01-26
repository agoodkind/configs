#!/bin/sh
# Cloudflared self-updating script for OPNsense
# This script checks for updates and automatically updates cloudflared

set -e

# Log function
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $*" | tee -a /var/log/cloudflared-update.log
}

log "Starting cloudflared update check"

# Get current version
CURRENT_VERSION=$(/usr/local/bin/cloudflared --version 2>/dev/null | head -1 | awk '{print $3}' || echo "unknown")

# Get latest version from GitHub API
LATEST_TAG=$(curl -s https://api.github.com/repos/cloudflare/cloudflared/releases/latest | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

if [ "$CURRENT_VERSION" != "$LATEST_TAG" ] && [ "$LATEST_TAG" != "" ]; then
    log "Updating from $CURRENT_VERSION to $LATEST_TAG"

    # Stop service
    service cloudflared stop

    # Backup current binary
    cp /usr/local/bin/cloudflared /usr/local/bin/cloudflared.backup

    # Build new version from source
    if /tmp/cloudflared-build.sh; then
        NEW_BINARY=$(cat /tmp/cloudflared-build.log | grep "Build successful" -A 1 | tail -1)
        if [ -f "$NEW_BINARY" ]; then
            mv "$NEW_BINARY" /usr/local/bin/cloudflared
            chmod +x /usr/local/bin/cloudflared

            # Test new binary
            if /usr/local/bin/cloudflared --version >/dev/null 2>&1; then
                log "Update successful"
                service cloudflared start
            else
                log "Update failed, restoring backup"
                mv /usr/local/bin/cloudflared.backup /usr/local/bin/cloudflared
                service cloudflared start
            fi
        else
            log "Build output not found"
            service cloudflared start
        fi
    else
        log "Build failed, restarting with old version"
        service cloudflared start
    fi
else
    log "Already at latest version $CURRENT_VERSION"
fi
