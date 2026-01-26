#!/bin/sh
# Setup cloudflared self-updating mechanism
# Installs update script and cron job

# Install update script
sudo cp /tmp/cloudflared-update.sh /usr/local/bin/update-cloudflared.sh
sudo chmod +x /usr/local/bin/update-cloudflared.sh

# Add to crontab (run daily at 2 AM)
echo "0 2 * * * root /usr/local/bin/update-cloudflared.sh" | sudo tee -a /etc/crontab > /dev/null

echo "Self-updating mechanism installed. Updates will run daily at 2 AM."
