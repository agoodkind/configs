# Cloudflared Installation on OPNsense

This directory contains scripts and configuration for cloudflared tunnel deployment on OPNsense.

## Installation Summary

**Date**: January 22, 2026
**Target**: agoodkind@3d06:bad:b01::1 (OPNsense router)
**Version**: cloudflared 2026.1.1 (built from source)

## Components Installed

### 1. Binary Installation

- **Location**: `/usr/local/bin/cloudflared`
- **Source**: kjake/cloudflared FreeBSD fork
- **Method**: Token-based authentication (no config file needed)

### 2. Service Configuration

- **RC Script**: `/usr/local/etc/rc.d/cloudflared`
- **Enabled in**: `/etc/rc.conf`
- **Features**:
  - Post-quantum encryption enabled
  - Automatic service management
  - Proper FreeBSD integration

### 3. Security

- **Token Storage**: `/usr/local/etc/cloudflared/token` (600 permissions)
- **Post-quantum encryption**: Enabled for enhanced security

### 4. Self-Updating

- **Build Script**: `/usr/local/bin/cloudflared-build.sh`
- **Update Script**: `/usr/local/bin/update-cloudflared.sh`
- **Schedule**: Daily at 2 AM via cron
- **Features**:
  - Builds latest version from official cloudflared source
  - Fixes FreeBSD build constraints automatically
  - Automatic version checking against GitHub releases
  - Backup of current binary
  - Rollback on failure
  - Logging to `/var/log/cloudflared-update.log` and `/var/log/cloudflared-build.log`

## Management Commands

### Service Control

```bash
# Check status
sudo service cloudflared status

# Restart service
sudo service cloudflared restart

# Stop service
sudo service cloudflared stop

# Start service
sudo service cloudflared start
```

### Monitoring

```bash
# View service logs
sudo tail -f /var/log/cloudflared.log

# View update logs
sudo tail -f /var/log/cloudflared-update.log

# Check version
cloudflared --version

# View metrics (if needed)
curl http://127.0.0.1:20241/metrics
```

### Manual Update

```bash
# Trigger manual update
sudo /usr/local/bin/update-cloudflared.sh
```

## Current Status

✅ **Service**: Running (PID varies)
✅ **Connections**: Multiple active to Cloudflare edge locations (sjc01, sjc07, sjc06, sjc11)
✅ **Encryption**: Post-quantum enabled
✅ **Auto-update**: Scheduled daily
✅ **Token**: Securely stored and validated

## Troubleshooting

### Service Won't Start

```bash
# Check rc.conf
grep cloudflared /etc/rc.conf

# Check token file
sudo ls -la /usr/local/etc/cloudflared/token

# Check logs
sudo tail -20 /var/log/cloudflared.log
```

### Update Issues

```bash
# Check update logs
sudo tail -20 /var/log/cloudflared-update.log

# Manual update
sudo /usr/local/bin/update-cloudflared.sh

# Restore backup if needed
sudo cp /usr/local/bin/cloudflared.backup /usr/local/bin/cloudflared
sudo service cloudflared restart
```

### Network Issues

```bash
# Test internet connectivity
curl -I https://github.com

# Check service status
sudo service cloudflared status
```

## Files in This Directory

- `cloudflared-token.sh` - Token setup script
- `cloudflared-rc.sh` - RC script installation
- `cloudflared-build.sh` - Build script for compiling from source
- `cloudflared-update.sh` - Self-update script (builds from source)
- `setup-cloudflared-updates.sh` - Update mechanism installer
- `cloudflared-status.sh` - Status checking script
- `README.md` - This documentation

## Build Process

The self-updating system now builds cloudflared from the official Cloudflare source code instead of relying on third-party binaries:

1. **Automatic Detection**: Checks GitHub releases for latest version
2. **Source Build**: Clones official repo and builds with Go
3. **FreeBSD Patches**: Automatically applies FreeBSD-specific build constraints
4. **Testing**: Verifies binary works before deployment
5. **Rollback**: Falls back to previous version on failure

## Notes

- Uses kjake/cloudflared fork for FreeBSD compatibility
- Token-based authentication (no cert files needed)
- Self-updating enabled with daily checks
- Post-quantum encryption for enhanced security
- All scripts stored in this directory for version control
