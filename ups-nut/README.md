# NUT Configuration Repository

This directory stores centralized Network UPS Tools (NUT) configurations for the infrastructure, enabling reproducible deployments and consistent monitoring across multiple hosts.

## Overview

- **Vault** (`3d06:bad:b01::254`): Primary hypervisor; NUT master
- **Suburban** (`3d06:bad:b01:200::254`): Secondary hypervisor; standalone or slave

Each host has its own UPS hardware, driver, and operational profile. Configurations are templated (Jinja2) to allow host-specific values while maintaining a single source of truth.

## Directory Structure

```
ups-nut/
├── PLAN.md                 # Full implementation roadmap
├── README.md               # This file
└── audit-nut.sh            # Script to audit existing NUT config on a host
```

## Quick Reference

### Files in `/etc/nut/` (deployed by Ansible)

| File            | Purpose                                       |
| --------------- | --------------------------------------------- |
| `ups.conf`      | UPS device definitions (driver, port, name)   |
| `upsmon.conf`   | Monitoring daemon (alerts, shutdown behavior) |
| `upsd.conf`     | Server binding (network exposure, auth)       |
| `upsd.users`    | Credentials (master/slave, read-only)         |
| `upssched.conf` | Event→command mappings                        |
| `nut.conf`      | Daemon mode (`master`, `standalone`, etc.)    |

### Scripts

- **`/opt/scripts/upssched-cmd`** (from Sites/scripts): Primary event handler
  - Monitors battery level, sends email alerts on power events
  - Integrates with `/opt/scripts/send-email`
- **`/usr/local/bin/nut-notify`**: Wrapper or symlink to above

## Deployment

### From Templates (Ansible)

```bash
# Deploy to all NUT-enabled hosts
ansible-playbook ansible/playbooks/deploy-nut.yml
```

The playbook:

1. Templates configs from `templates/` → `/etc/nut/`
2. Installs notification scripts to `/usr/local/bin/`
3. Restarts `upsd` and `upsmon` services
4. Verifies connectivity with `upsc`

### Manual Verification

```bash
# Query UPS status (local)
upsc vault-ups

# Check daemon status
systemctl status upsd upsmon

# View logs
journalctl -u upsd -u upsmon -n 50

# Test scheduler (force event)
/usr/local/bin/upssched-cmd ups-on-battery
```

## Configuration Notes

### Vault (Master)

- Actively monitors the primary UPS via driver (e.g., `apcupsd` or `nutdrv_qx`)
- Exposes `upsd` on `[::]:3493` for network access
- Makes shutdown decisions (Forced Shutdown / FSD)
- Runs full alerting via `upssched-cmd`

### Suburban (Standalone or Slave)

- May operate independently (standalone) or pull from vault (slave)
- Alert sensitivity may differ from vault
- Notification behavior configured per operational needs

See `vault/VAULT_NOTES.md` and `suburban/SUBURBAN_NOTES.md` for host-specific details.

## Permissions

All NUT configs must be restrictive:

```
/etc/nut/ups.conf       600 root:nut
/etc/nut/upsmon.conf    600 root:nut
/etc/nut/upsd.conf      640 root:nut
/etc/nut/upsd.users     600 root:nut
/etc/nut/upssched.conf  640 root:nut
/etc/nut/nut.conf       644 root:nut
```

Credentials (passwords, keys) stored in `ansible/inventory/group_vars/all/vault.yml` (encrypted).

## Troubleshooting

### UPS Not Detected

1. Verify device connection: `ls -la /dev/ttyUSB* /dev/hidraw*`
2. Check driver configuration: `cat /etc/nut/ups.conf | grep -A3 "^\["`
3. Review logs: `journalctl -u upsd -n 50`

### No Network Access to upsd

1. Verify binding: `cat /etc/nut/upsd.conf | grep -i listen`
2. Check firewall: Is port 3493 open? `ufw status | grep 3493`
3. Verify credentials: Does user exist in `/etc/nut/upsd.users`?

### Alerts Not Sending

1. Test notification script: `/usr/local/bin/nut-notify ups-on-battery`
2. Check email config in `/opt/scripts/send-email`
3. Review logs: `journalctl -u upssched -n 20` or `/var/log/upssched*`

## Maintenance

### Adding a New Host

1. Capture current configs from `/etc/nut/` on the new host
2. Add host-specific vars to `ansible/inventory/group_vars/<hostname>_servers.yml`
3. Run `deploy-nut.yml` with target filter: `--limit <hostname>`
4. Document in `<hostname>/NOTES.md`

### Updating Credentials

1. Edit `ansible/inventory/group_vars/all/vault.yml` (encrypted)
2. Rerun playbook: `ansible-playbook ansible/playbooks/deploy-nut.yml`
3. Verify with `upsc <ups_name>`

### Testing Events

To simulate power events for testing alerts:

```bash
# Simulate battery-low (triggers shutdown logic)
upssched-cmd ups-low-battery

# Simulate power return
upssched-cmd ups-back-on-line

# Monitor battery drain in real-time
upssched-cmd ups-on-battery
```

## References

- [NUT Official Docs](https://networkupstools.org/docs.html)
- [RFC on UPS Monitoring](https://tools.ietf.org/html/rfc3162)
- See also: `/opt/scripts/upssched-cmd` for alert integration details

## Implementation Status

- [ ] Audit current configs (Phase 1)
- [ ] Create backups and host notes (Phase 2-3)
- [ ] Finalize templates and variables (Phase 4)
- [ ] Develop Ansible playbook (Phase 5)
- [ ] Test on single host (Phase 6)
- [ ] Roll out to all hosts (Phase 7)

See `PLAN.md` for detailed steps.
