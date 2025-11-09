#!/usr/bin/env bash
# Run this script to set up the Proxmox API token on ansible server

echo "This script will help you set the PROXMOX_API_TOKEN on ansible.home.goodkind.io"
echo ""
echo "Please paste your Proxmox API token (it will not be echoed):"
read -s TOKEN

if [ -z "$TOKEN" ]; then
    echo "No token provided. Exiting."
    exit 1
fi

echo ""
echo "Setting token on ansible.home.goodkind.io..."

ssh root@ansible.home.goodkind.io "echo 'export PROXMOX_API_TOKEN=\"$TOKEN\"' >> ~/.bashrc && echo 'Token added to ~/.bashrc'"

echo ""
echo "✅ Done! The token is now set and will persist across sessions."
echo ""
echo "To test immediately (in current session):"
echo "  ssh root@ansible.home.goodkind.io"
echo "  export PROXMOX_API_TOKEN='your-token'"
echo "  cd /root/ansible && ansible-inventory -i inventory --list"

