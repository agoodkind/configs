---
description: Deploy Ansible playbooks locally with proper configuration
command: |
  #!/usr/bin/env bash
  set -euo pipefail

  # Colors for output
  RED='\033[0;31m'
  GREEN='\033[0;32m'
  YELLOW='\033[1;33m'
  BLUE='\033[0;34m'
  NC='\033[0m' # No Color

  # Function to print colored output
  print_error() {
      echo -e "${RED}❌ $1${NC}" >&2
  }

  print_success() {
      echo -e "${GREEN}✅ $1${NC}"
  }

  print_info() {
      echo -e "${BLUE}ℹ️  $1${NC}"
  }

  print_warning() {
      echo -e "${YELLOW}⚠️  $1${NC}"
  }

  # Get playbook name from user input
  if [ $# -eq 0 ]; then
      print_error "Please specify a playbook to deploy"
      echo >&2
      print_info "Usage: deploy-playbook <playbook.yml>" >&2
      echo >&2
      print_info "Available deploy playbooks:" >&2
      cd ansible/playbooks 2>/dev/null || { print_error "Cannot find ansible/playbooks directory"; exit 1; }
      for playbook in deploy-*.yml; do
          if [ -f "$playbook" ]; then
              echo "  • $playbook" >&2
          fi
      done
      echo >&2
      print_info "Example: deploy-playbook deploy-proxy.yml" >&2
      exit 1
  fi

  PLAYBOOK="$1"
  PLAYBOOK_PATH="ansible/playbooks/$PLAYBOOK"

  # Check if playbook exists
  if [ ! -f "$PLAYBOOK_PATH" ]; then
      print_error "Playbook not found: $PLAYBOOK_PATH"
      echo >&2
      print_info "Available playbooks:" >&2
      if [ -d "ansible/playbooks" ]; then
          ls -1 ansible/playbooks/*.yml 2>/dev/null | while read -r pb; do
              echo "  • $(basename "$pb")" >&2
          done
      fi
      exit 1
  fi

  # Check if ansible.cfg exists
  if [ ! -f "ansible/ansible.cfg" ]; then
      print_error "ansible.cfg not found in ansible/ directory"
      print_info "This command must be run from the configs repository root" >&2
      exit 1
  fi

  # Check if vault password file exists
  if [ ! -f "$HOME/.config/ansible/vault.pass" ]; then
      print_error "Vault password file not found at ~/.config/ansible/vault.pass"
      print_info "Ensure your Ansible vault password is set up correctly" >&2
      exit 1
  fi

  print_info "Running deploy playbook: $PLAYBOOK"
  echo

  # Change to ansible directory and run playbook
  cd ansible
  exec ansible-playbook "playbooks/$PLAYBOOK"
---

# Deploy Ansible Playbook

Run Ansible deploy playbooks locally with proper configuration and vault password handling.

## Usage

- `deploy-playbook deploy-proxy.yml` - Deploy the proxy container
- `deploy-playbook deploy-adguard.yml` - Deploy AdGuard
- `deploy-playbook deploy-mwan.yml` - Deploy MWAN configuration

## What it does

1. **Validates** the playbook exists in `ansible/playbooks/`
2. **Checks** that `ansible/ansible.cfg` exists for proper configuration
3. **Verifies** vault password file exists at `~/.config/ansible/vault.pass`
4. **Changes** to the `ansible/` directory (required for config)
5. **Runs** `ansible-playbook playbooks/<playbook>` with vault decryption

## Available playbooks

- `deploy-adguard.yml` - AdGuard Home DNS server
- `deploy-dns64.yml` - DNS64 configuration
- `deploy-grommunio.yml` - Grommunio email server
- `deploy-mwan.yml` - Multi-WAN configuration
- `deploy-nanomdm.yml` - NanoMDM device management
- `deploy-powerdns.yml` - PowerDNS server
- `deploy-proxy.yml` - Traefik reverse proxy + SSHPiper + Cloudflared
- `deploy-semaphore.yml` - Semaphore automation server
- `deploy-ssh-keys.yml` - SSH key deployment

## Why this is needed

Ansible requires running from the directory containing `ansible.cfg` to pick up:
- Vault password file location
- Inventory paths
- Plugin configurations
- Other Ansible settings

Running from the parent directory causes vault decryption failures.
