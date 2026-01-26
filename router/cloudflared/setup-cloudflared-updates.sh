#!/bin/sh
# Setup cloudflared self-updating mechanism on router
# This is DEPRECATED - use automated building on freebsd-dev instead
#
# For new deployments, use setup-freebsd-dev.sh on freebsd-dev
# This script remains for backward compatibility

echo "WARNING: This script is deprecated for new deployments."
echo "Use automated building on freebsd-dev with setup-freebsd-dev.sh instead."
echo ""
echo "If you must use this legacy method, ensure you have:"
echo "1. CLOUDFLARED_TOKEN set in environment"
echo "2. cloudflared-token.sh, cloudflared-build.sh, and cloudflared-update.sh in same directory"
echo ""

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Install build script
sudo cp "$SCRIPT_DIR/cloudflared-build-router.sh" /usr/local/bin/cloudflared-build-router.sh
sudo chmod +x /usr/local/bin/cloudflared-build-router.sh

# Install update script
sudo cp "$SCRIPT_DIR/cloudflared-update.sh" /usr/local/bin/update-cloudflared.sh
sudo chmod +x /usr/local/bin/update-cloudflared.sh

# Add to crontab (run daily at 2 AM) - but mark as legacy
CRON_ENTRY="# Legacy cloudflared update (consider migrating to freebsd-dev automation)
0 2 * * * root /usr/local/bin/update-cloudflared.sh"

if ! grep -q "update-cloudflared.sh" /etc/crontab 2>/dev/null; then
    echo "$CRON_ENTRY" | sudo tee -a /etc/crontab > /dev/null
fi

echo "Legacy self-updating mechanism installed."
echo "Updates will run daily at 2 AM."
echo ""
echo "MIGRATION RECOMMENDED: Consider setting up automated builds on freebsd-dev instead."
