#!/usr/bin/env bash
# Generate PowerDNS secrets for Semaphore environment variables
# Run this manually: ./generate-pdns-secrets.sh

set -euo pipefail

echo "Generating PowerDNS secrets for Semaphore..."
echo ""

# Generate secrets using ansible's password lookup
PDNS_POSTGRES_PASSWORD=$(ansible localhost -m debug -a "msg={{ lookup('password', '/dev/null length=32 chars=ascii_letters,digits') }}" 2>/dev/null | grep -oP '"msg": "\K[^"]+')
PDNS_API_KEY=$(ansible localhost -m debug -a "msg={{ lookup('password', '/dev/null length=32 chars=ascii_letters,digits') }}" 2>/dev/null | grep -oP '"msg": "\K[^"]+')
PDNS_TSIG_SECRET=$(ansible localhost -m debug -a "msg={{ lookup('password', '/dev/null length=44 chars=ascii_letters,digits,+,/=') }}" 2>/dev/null | grep -oP '"msg": "\K[^"]+')

cat <<EOF
================================================================================
Generated PowerDNS Secrets - Add these to Semaphore Environment Variables
================================================================================

PDNS_POSTGRES_PASSWORD=${PDNS_POSTGRES_PASSWORD}
PDNS_API_KEY=${PDNS_API_KEY}
PDNS_TSIG_SECRET=${PDNS_TSIG_SECRET}

To add in Semaphore:
1. Go to Environments → Select your environment
2. Add these as Environment Variables
3. Save and run the deploy-powerdns.yml playbook

================================================================================
EOF

# Optionally write to file
read -p "Write to /tmp/pdns-secrets.txt? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    cat <<EOF > /tmp/pdns-secrets.txt
PDNS_POSTGRES_PASSWORD=${PDNS_POSTGRES_PASSWORD}
PDNS_API_KEY=${PDNS_API_KEY}
PDNS_TSIG_SECRET=${PDNS_TSIG_SECRET}
EOF
    chmod 0600 /tmp/pdns-secrets.txt
    echo "✓ Written to /tmp/pdns-secrets.txt (permissions: 0600)"
fi

