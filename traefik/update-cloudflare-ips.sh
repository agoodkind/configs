#!/usr/bin/env bash
set -euo pipefail

# Update Cloudflare IP ranges in Traefik configuration
# Run this periodically to keep IP ranges up to date
# Cloudflare IPs from: https://www.cloudflare.com/ips/

echo "=== Updating Cloudflare IP ranges ==="

# Fetch current Cloudflare IPs
echo "Fetching Cloudflare IPv4 ranges..."
CF_IPV4=$(curl -s https://www.cloudflare.com/ips-v4)

echo "Fetching Cloudflare IPv6 ranges..."
CF_IPV6=$(curl -s https://www.cloudflare.com/ips-v6)

# Generate the IP list for middlewares (with proper YAML formatting)
echo
ipv6_count=$(echo "$CF_IPV6" | wc -l | tr -d ' ')
echo "IPv6 ranges ($ipv6_count total):"
echo "$CF_IPV6" | sed 's/^/          - "/' | sed 's/$/"/'
echo

ipv4_count=$(echo "$CF_IPV4" | wc -l | tr -d ' ')
echo "IPv4 ranges ($ipv4_count total):"
echo "$CF_IPV4" | sed 's/^/          - "/' | sed 's/$/"/'
echo

echo "✅ Copy and paste the above into:"
echo "   - traefik/dynamic/middlewares.yml.j2 (cloudflare-only middleware)"
echo "   - traefik/traefik.yml.j2 (forwardedHeaders.trustedIPs)"
echo
echo "💡 Tip: Run this script monthly to keep IPs current"
echo "   Add to crontab: 0 0 1 * * /path/to/update-cloudflare-ips.sh | mail -s 'Update Cloudflare IPs' admin@example.com"
