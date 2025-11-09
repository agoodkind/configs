# Logstash Firewall Log Parser

## Overview

Custom Ruby filter and Logstash pipeline configuration for parsing and enriching firewall logs (pfSense/OPNsense filterlog format).

## Environment Configuration

Project supports environment variables via `.env` file. See `.env.example` for required and optional variables.

Ruby version is managed via `.ruby-version` (set to `3.4.7`).

## Prerequisites

## Quickstart

1. Copy `.env.example` to `.env` and update values as needed.
2. Ensure Ruby version matches `.ruby-version` (3.4.7).
3. Install dependencies and run tasks as below.

## Structure

```
ruby/
  parse_filterlog.rb             # Custom Ruby filter for filterlog parsing
conf/
  01-inputs.logstash.conf        # Input configuration (syslog, http)
  10-syslog-header.logstash.conf # Syslog header parsing
  20-fw-filterlog.logstash.conf  # Firewall filterlog parsing
  21-unbound.logstash.conf       # Unbound DNS log parsing
  22-bind.logstash.conf          # BIND DNS log parsing
  30-enrichment.logstash.conf    # Timestamp enrichment
  99-outputs.logstash.conf       # Output configuration (Elasticsearch)
spec/
  parse_filterlog_spec.rb        # RSpec test suite for filterlog parser
test_data/
  sample_logs.txt                # Sample log data
.env.example                     # Example environment variable file
.env                             # Local environment variable file
.ruby-version                    # Ruby version file
Gemfile                          # Ruby dependencies
Gemfile.lock                     # Dependency lock file
Makefile                         # Ruby environment initialization
Rakefile                         # Project tasks and deployment
LICENSE                          # License
README.md                        # Project documentation
packetfilter_description.txt      # Filterlog field reference
.rubocop.yml                     # RuboCop linting configuration
.gitignore                       # Git ignore rules
.vscode/
  settings.json                  # VSCode editor settings
  extensions.json                # VSCode recommended extensions
.ruby-lsp/                       # Ruby LSP configuration (if present)
```

## Setup

```bash
# Install dependencies
rake install

# Run development checks
rake dev
```

## Development Tasks

- `rake install` — Install Ruby dependencies
- `rake test` — Run RSpec tests
- `rake lint` — Run RuboCop linter
- `rake format` — Auto-format code
- `rake dev` — Run format, lint, and test
- `rake clean` — Remove temporary files

## Deployment Tasks

- `rake deploy` — Deploy Ruby filter and configs (with backup)
- `rake dry_run` — Preview deployment without making changes
- `rake backup` — Create manual backup of current configs
- `rake check` — Validate Logstash configuration
- `rake restart` — Restart Logstash service
- `rake full_deploy` — Complete pipeline (format, lint, test, backup, deploy, check)

## Diagnostic Tasks

- `rake diagnose` — Check for common configuration issues
- `rake status` — Show Logstash service status
- `rake logs` — Tail Logstash logs
- `rake check_port` — Check if port 5140 is available
- `rake fix_duplicates` — Find duplicate input configurations

## Deployment Workflow

```bash
# Full automated deployment
rake full_deploy

# Then restart Logstash
rake restart

# Monitor for issues
rake logs
```

## Manual Deployment Steps

```bash
# 1. Run development checks
rake dev

# 2. Backup and deploy
rake deploy

# 3. Diagnose issues
rake diagnose

# 4. Validate configuration
rake check

# 5. Restart service
rake restart

# 6. Monitor logs
rake logs
```

## Dry-Run Mode

Preview changes without applying them:

```bash
DRY_RUN=1 rake deploy
```

## Remote Deployment

Deploy to a remote Logstash server via SSH using the `REMOTE=1` environment variable.

### Configuration

Set the remote host via environment variable or edit the default in `Rakefile`:

```bash
# Option 1: Use environment variable (recommended)
export REMOTE_HOST='user@hostname'

# Option 2: Edit default in Rakefile
REMOTE_HOST = ENV['REMOTE_HOST'] || 'root@10.250.0.4'
```

### Remote Deployment Commands

```bash
# Deploy to default remote server
REMOTE=1 rake deploy

# Deploy to custom remote host
REMOTE=1 REMOTE_HOST='user@192.168.1.100' rake deploy

# Restart Logstash on remote host
REMOTE=1 rake restart

# Run diagnostics on remote server
REMOTE=1 rake diagnose

# Check remote configuration
REMOTE=1 rake check

# View remote logs
REMOTE=1 rake logs
```

### Remote Deployment Workflow

```bash
# 1. Run local tests
rake dev

# 2. Preview remote deployment
REMOTE=1 DRY_RUN=1 rake deploy

# 3. Deploy to custom remote server
REMOTE=1 REMOTE_HOST='user@logstash.example.com' rake deploy

# 4. Validate remote configuration
REMOTE=1 rake check

# 5. Restart remote service
REMOTE=1 rake restart

# 6. Monitor remote logs
REMOTE=1 rake logs
```

## Proxmox LXC Deployment

Deploy to a Proxmox LXC container using `pct` commands via SSH to the Proxmox host.

### Configuration

Set both Proxmox host and container VMID:

```bash
export PROXMOX_HOST='root@proxmox.example.com'
export PROXMOX_VMID='100'
```

### Proxmox Deployment Commands

```bash
# Deploy to Proxmox LXC container
PROXMOX_HOST='root@pve' PROXMOX_VMID='100' rake deploy

# Restart Logstash in container
PROXMOX_HOST='root@pve' PROXMOX_VMID='100' rake restart

# View logs from container
PROXMOX_HOST='root@pve' PROXMOX_VMID='100' rake logs

# Run diagnostics in container
PROXMOX_HOST='root@pve' PROXMOX_VMID='100' rake diagnose
```

### Proxmox Deployment Workflow

```bash
# Set environment variables
export PROXMOX_HOST='root@pve.home'
export PROXMOX_VMID='100'

# 1. Run local tests
rake dev

# 2. Preview deployment
DRY_RUN=1 rake deploy

# 3. Deploy to container
rake deploy

# 4. Validate configuration
rake check

# 5. Restart service
rake restart

# 6. Monitor logs
rake logs
```

### How Proxmox Deployment Works

- Files are copied to Proxmox host via `scp`, then pushed into container with `pct push`
- Commands execute inside container via `pct exec VMID -- command`
- Requires SSH access to Proxmox host
- Proxmox host must have access to the specified container VMID

### How Remote Deployment Works

- Uses `scp` to copy files to remote server
- Executes commands via `ssh` on remote host
- Supports all rake tasks (deploy, diagnose, check, restart, logs, etc.)
- Works with dry-run mode: `REMOTE=1 DRY_RUN=1 rake deploy`

### Requirements

- SSH access configured (password or key-based auth)
- Appropriate sudo permissions on remote host
- Remote paths must exist: `/etc/logstash/ruby/` and `/etc/logstash/conf.d/`

## Notes

- Automatic backups are created before deployment with timestamp
- All deployed files are owned by `logstash:logstash` with 644 permissions
- Port 5140 is used for syslog input (UDP)