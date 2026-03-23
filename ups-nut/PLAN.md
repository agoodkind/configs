# NUT Configuration Storage Plan

## Overview

This plan establishes a centralized structure for managing NUT (Network UPS Tools) configurations across multiple hosts, starting with `vault` and `suburban` hypervisors. The goal is to capture current configs, document host-specific differences, and provide a repeatable deployment process.

---

## 1. Directory Structure

```
configs/ups-nut/
├── PLAN.md                          # This file - implementation guide
├── README.md                         # Deployment & maintenance notes
├── templates/                        # Templated configs (Jinja2, host-agnostic)
│   ├── ups.conf.j2
│   ├── upsmon.conf.j2
│   ├── upsd.conf.j2
│   ├── upsd.users.j2
│   ├── upssched.conf.j2
│   ├── nut.conf.j2
│   └── notify.sh.j2
│
├── vault/                           # Vault hypervisor (Proxmox host)
│   ├── ups.conf
│   ├── upsmon.conf
│   ├── upsd.conf
│   ├── upsd.users
│   ├── upssched.conf
│   ├── nut.conf
│   ├── notify.sh
│   ├── ups.conf.dpkg-dist            # (optional) original from package
│   └── VAULT_NOTES.md               # Host-specific notes
│
└── suburban/                        # Suburban hypervisor
    ├── ups.conf
    ├── upsmon.conf
    ├── upsd.conf
    ├── upsd.users
    ├── upssched.conf
    ├── nut.conf
    ├── notify.sh
    ├── ups.conf.dpkg-dist            # (optional) original from package
    └── SUBURBAN_NOTES.md             # Host-specific notes
```

---

## 2. Files to Capture from `/etc/nut/`

### Core Configuration Files

| File | Purpose | Deployed By | Notes |
|------|---------|-------------|-------|
| `ups.conf` | UPS device definitions (USB serial, network addresses) | Ansible template | Host-specific UPS names, driver selections |
| `upsmon.conf` | Monitoring daemon config (master/slave, alert thresholds, power-fail behavior) | Ansible template | Different alert settings per host |
| `upsd.conf` | Server binding config (listen addresses, auth method) | Ansible template | IPv6/IPv4 interfaces differ; network exposure |
| `upsd.users` | Authentication credentials (local/remote users) | Ansible template + vault | Credentials stored in `all/vault.yml` |
| `upssched.conf` | Scheduler config (event → command mapping) | Ansible template | Command paths vary by host |
| `nut.conf` | Global NUT daemon mode (`standalone`, `master`, `slave`) | Ansible template | `vault` is master; `suburban` may be slave/standalone |
| `notify.sh` | Custom notification script (email alerts, event handling) | Copy from `/opt/scripts/` | Wraps `/opt/scripts/send-email` for email delivery |

### Optional Archive Files

| File | Purpose | Notes |
|------|---------|-------|
| `ups.conf.dpkg-dist` | Original from Debian package | Reference for default settings; helps identify customizations |
| `upsmon.conf.dpkg-dist` | Original from Debian package | Reference for default alert levels |
| `upsd.conf.dpkg-dist` | Original from Debian package | Reference for default listeners |

---

## 3. Key Host Differences

### Vault (Proxmox host at `3d06:bad:b01::254`)

**Role**: Primary hypervisor, NUT master for all UPS monitoring on the network.

**UPS Configuration**:
- UPS device: `[vault-ups]` (APC Smart-UPS, typically connected via USB at `/dev/ttyUSB0` or similar)
- Driver: `apcupsd` or `nutdrv_qx` (depending on model)
- Network interface: Binds to `[::]:3493` (IPv6 all-interfaces) for upsd
- Connection pipes: `/run/nut/` (standard)

**System Details**:
- Hostname: `vault.home.goodkind.io`
- IPv6: `3d06:bad:b01::254`
- Mode: `master` (actively monitors UPS, makes shutdown decisions)
- Monitoring thresholds: Aggressive (low battery → shutdown earlier)
- Upssched role: Runs `upssched-cmd` for email alerts and logging

**Network Exposure**:
- Opens upsd port 3493 on IPv6 for slave monitoring from other hosts
- Authentication: Master credentials in `upsd.users` for intra-network access

**Example `ups.conf` section**:
```
[vault-ups]
driver = apcupsd
port = /dev/ttyUSB0
desc = "Vault Hypervisor UPS"
```

**Example `nut.conf` entry**:
```
MODE=master
```

---

### Suburban (Remote hypervisor at `3d06:bad:b01::114`)

**Role**: Secondary/remote hypervisor; may run as standalone or slave to vault master.

**UPS Configuration**:
- UPS device: `[suburban-ups]` (model/brand TBD; may differ from vault)
- Driver: Determined by hardware (common: `nutdrv_qx`, `richcomm_usb`, etc.)
- Network interface: Binds locally only (if standalone) or pulls from vault (if slave)
- Connection pipes: `/run/nut/` (standard)

**System Details**:
- Hostname: `hypervisor.suburban.goodkind.io`
- IPv6: `3d06:bad:b01::114`
- Mode: `standalone` or `slave` (depends on network connectivity and UPS availability)
- Monitoring thresholds: May be looser (backup system, less critical)
- Upssched role: Runs scaled-down event notifications (or disabled if slave)

**Network Exposure**:
- If standalone: Opens upsd port 3493 only to localhost/management network
- If slave: Does not open upsd; connects to vault's upsd as a client
- Authentication: Slave credentials in local `upsd.users` if standalone

**Example `ups.conf` section** (if standalone):
```
[suburban-ups]
driver = nutdrv_qx
port = /dev/ttyUSB0
desc = "Suburban Hypervisor UPS"
```

**Example `nut.conf` entry** (if standalone):
```
MODE=standalone
```

---

## 4. Configuration Differences Summary

### Dimensions of Variation

| Aspect | Vault | Suburban | Impact |
|--------|-------|----------|--------|
| **UPS Name** | `vault-ups` | `suburban-ups` (or TBD) | Aliases in upsd.users, scheduler commands |
| **Device Port** | `/dev/ttyUSB0` (or detected) | `/dev/ttyUSB0` (or detected) | Hardware-specific; must match actual device |
| **Driver** | `apcupsd` or `nutdrv_qx` | Determined by UPS model | Affects protocol and feature support |
| **Daemon Mode** | `master` | `standalone` or `slave` | Network topology; shutdown authority |
| **Upsd Binding** | `[::]:3493` (public to network) | `127.0.0.1:3493` or None (if slave) | Security; who can query this UPS |
| **Pipe/Lock Paths** | `/run/nut/` (standard) | `/run/nut/` (standard) | NUT daemon infrastructure |
| **Alert Sensitivity** | Aggressive (lower thresholds) | Conservative (higher thresholds) | UPS operational profile |
| **Notify Script Behavior** | Sends alerts to all systems | May be disabled or simplified | Email/logging volume |
| **Credentials** | Master-level access (`upsmon`) | Master or read-only | Determines monitoring scope |

### Template Variables (for Jinja2 Templates)

Each template should accept:

```jinja2
{# Common variables #}
ups_name: "{{ ups_name }}"              # e.g., "vault-ups" or "suburban-ups"
ups_driver: "{{ ups_driver }}"          # e.g., "apcupsd", "nutdrv_qx"
ups_port: "{{ ups_port }}"              # e.g., "/dev/ttyUSB0"
ups_desc: "{{ ups_desc }}"              # Friendly name

daemon_mode: "{{ daemon_mode }}"        # "master", "slave", "standalone"
upsd_bind: "{{ upsd_bind }}"            # "[::]:3493" or "127.0.0.1:3493"

alert_low_battery_pct: "{{ alert_low_battery_pct }}"  # 10-25%, host-specific
alert_shutdown_delay: "{{ alert_shutdown_delay }}"    # Seconds before FSD

notify_email: "{{ notify_email }}"      # Email recipient (from vault)
notify_script_path: "{{ notify_script_path }}"  # "/usr/local/bin/nut-notify"

master_host: "{{ master_host }}"        # IPv6 of vault (for slaves)
master_port: "{{ master_port }}"        # 3493 (standard)
```

---

## 5. README: Deployment & Maintenance Notes

### Permissions & Ownership

All NUT configs must be owned by `root` and protected from world-readable access:

```bash
/etc/nut/ups.conf          600  root:nut
/etc/nut/upsmon.conf       600  root:nut
/etc/nut/upsd.conf         640  root:nut
/etc/nut/upsd.users        600  root:nut
/etc/nut/upssched.conf     640  root:nut
/etc/nut/nut.conf          644  root:nut
/usr/local/bin/nut-notify  755  root:root  (if custom script)
```

### Script Sources

- **`upssched-cmd`** (notification handler): Sourced from `/opt/scripts/upssched-cmd` (Sites/scripts)
  - Integrates with `/opt/scripts/send-email` for email delivery
  - Monitors battery percentage, sends alerts on power events
  - Deployed to `/usr/local/bin/nut-notify` or symlinked

- **`notify.sh`** (legacy/wrapper): May be generated from template
  - Simple wrapper calling `upssched-cmd`
  - Scheduled via `upssched.conf`

- **Email integration**: Via `/opt/scripts/send-email` (not NUT-owned)
  - Deployed separately by infrastructure playbooks
  - Called with flags: `-t <email>`, `-s <subject>`, `-m <message>`, etc.

### Deployment Flow

1. **Create templates** in `ups-nut/templates/` with Jinja2 variables
2. **Create host-specific group_vars** in `ansible/inventory/group_vars/vault_servers.yml` and `suburban_servers.yml`
3. **Deploy via Ansible playbook** (e.g., `ansible/playbooks/deploy-nut.yml`)
   - Template each file from `templates/` → `/etc/nut/`
   - Copy notification scripts to `/usr/local/bin/`
   - Restart NUT services (`upsd`, `upsmon`)
4. **Verify** with `upsc` and `upsmon -c status`

### Maintenance

- **Rotating logs**: Configure logrotate or systemd journal
- **Testing alerts**: Trigger events with `upsc` queries or simulate power failure
- **Credential rotation**: Update `upsd.users` in vault, redeploy
- **Hardware swap**: Update `ups.conf` (port, driver), redeploy

---

## 6. Implementation Steps (Sequential)

### Phase 1: Audit Current State

```bash
# On vault (SSH to 3d06:bad:b01::254 or via proxy)
ssh root@3d06:bad:b01::254
cat /etc/nut/ups.conf
cat /etc/nut/upsmon.conf
cat /etc/nut/upsd.conf
cat /etc/nut/upsd.users
cat /etc/nut/upssched.conf
cat /etc/nut/nut.conf
ls -la /usr/bin/nut* /usr/local/bin/*nut* 2>/dev/null
cat /opt/scripts/upssched-cmd  # (if exists)

# On suburban (SSH to 3d06:bad:b01::114)
ssh root@3d06:bad:b01::114
# Repeat config inspection
```

### Phase 2: Create Baseline Backups

```bash
# Create directory structure
mkdir -p ~/Sites/configs/ups-nut/{vault,suburban,templates}

# Backup from vault
scp root@3d06:bad:b01::254:/etc/nut/* ~/Sites/configs/ups-nut/vault/

# Backup from suburban
scp root@3d06:bad:b01::114:/etc/nut/* ~/Sites/configs/ups-nut/suburban/

# Optional: capture .dpkg-dist originals
scp root@3d06:bad:b01::254:/etc/nut/*.dpkg-dist ~/Sites/configs/ups-nut/vault/ 2>/dev/null || true
scp root@3d06:bad:b01::114:/etc/nut/*.dpkg-dist ~/Sites/configs/ups-nut/suburban/ 2>/dev/null || true

# Backup notification scripts
scp root@3d06:bad:b01::254:/opt/scripts/upssched-cmd ~/Sites/configs/ups-nut/
scp root@3d06:bad:b01::254:/usr/local/bin/nut-notify ~/Sites/configs/ups-nut/ 2>/dev/null || true
```

### Phase 3: Extract Variables & Document

1. **Analyze each config file** to identify:
   - UPS names, drivers, ports
   - Network bindings and credentials
   - Alert thresholds and behaviors
   - Command paths in scheduler

2. **Create host-specific notes**:
   - `vault/VAULT_NOTES.md` (role, UPS hardware details, special settings)
   - `suburban/SUBURBAN_NOTES.md` (same for suburban)

3. **Identify Ansible group requirements**:
   - Create `ansible/inventory/group_vars/vault_servers.yml`
   - Create `ansible/inventory/group_vars/suburban_servers.yml`
   - List required variables for templates

### Phase 4: Create Jinja2 Templates

For each `.conf` file in `/etc/nut/`, create a `.j2` template:

- `templates/ups.conf.j2` — UPS device definitions
- `templates/upsmon.conf.j2` — Monitoring daemon config
- `templates/upsd.conf.j2` — Server binding config
- `templates/upsd.users.j2` — Authentication (with vault integration)
- `templates/upssched.conf.j2` — Scheduler rules
- `templates/nut.conf.j2` — Daemon mode
- `templates/notify.sh.j2` — Notification handler (or just copy from `/opt/scripts/upssched-cmd`)

### Phase 5: Create Ansible Playbook

Create `ansible/playbooks/deploy-nut.yml`:

```yaml
---
- name: Deploy NUT Configuration
  hosts: vault_servers, suburban_servers
  become: yes
  vars_files:
    - inventory/group_vars/all/vault.yml
  tasks:
    - name: Template NUT configs to /etc/nut/
      ansible.builtin.template:
        src: "ups-nut/templates/{{ item }}.j2"
        dest: "/etc/nut/{{ item }}"
        mode: "0640"
        owner: root
        group: nut
      loop:
        - ups.conf
        - upsmon.conf
        - upsd.conf
        - upsd.users
        - upssched.conf
        - nut.conf
      notify: Restart NUT services

    - name: Install notification script
      ansible.builtin.copy:
        src: "{{ repo_root }}/ups-nut/notify.sh"
        dest: /usr/local/bin/nut-notify
        mode: "0755"
        owner: root
        group: root

    - name: Restart upsd and upsmon
      ansible.builtin.systemd:
        name: "{{ item }}"
        state: restarted
      loop:
        - upsd
        - upsmon

    - name: Verify NUT status
      ansible.builtin.shell: upsc vault-ups
      register: nut_status
      changed_when: false

    - name: Display NUT status
      ansible.builtin.debug:
        var: nut_status.stdout_lines
```

### Phase 6: Test & Iterate

1. **Deploy to one host** (e.g., vault) first
2. **Verify services restart** cleanly
3. **Test monitoring** (run `upsc vault-ups`, check `journalctl -u upsmon`)
4. **Test notifications** (if applicable)
5. **Deploy to other hosts** after validation

### Phase 7: Commit & Document

1. Commit all backups and templates to git
2. Document any deviations in host-specific `NOTES.md` files
3. Update this `PLAN.md` with final variable list and playbook output
4. Create README.md with usage and troubleshooting

---

## 7. Verification Checklist

After deployment:

- [ ] All config files exist in `/etc/nut/` with correct permissions
- [ ] `upsd` service is running: `systemctl status upsd`
- [ ] `upsmon` service is running: `systemctl status upsmon`
- [ ] `upsc <ups_name>` returns valid UPS data
- [ ] Notification script is executable: `ls -la /usr/local/bin/nut-notify`
- [ ] Logs show no permission or binding errors: `journalctl -u upsd -u upsmon`
- [ ] Remote NUT query succeeds (if master): `upsc vault-ups@vault.home.goodkind.io`
- [ ] Test email alert (if configured): Trigger via `upssched-cmd` manually

---

## 8. Next Steps

1. **Read this plan** and confirm scope with team
2. **Execute Phase 1** (audit current state) and gather actual configs
3. **Create Phase 2-3 backups** and analysis documents
4. **Proceed with templating and playbook development** once variables are finalized
5. **Schedule test deployment** on a non-critical host first
6. **Gradual rollout** across vault, then suburban, then any additional hosts

---

## Appendix: Common NUT Terminology

- **upsd**: Network daemon exposing UPS status on a network port
- **upsmon**: Monitoring daemon that watches upsd and triggers alarms/shutdown
- **upscheck**: Status verification command (deprecated; use `upsc` instead)
- **upsc**: Query command to check UPS status and variables
- **upssched**: Scheduler that maps UPS events to external commands
- **Master mode**: Host with direct UPS connection; can initiate forced shutdown (FSD)
- **Slave mode**: Host querying remote upsd on another host; read-only
- **Standalone mode**: Single host, no network exposure; local-only UPS connection
- **FSD (Forced Shutdown)**: Command issued by master to all slaves, triggering system shutdown
- **Powerchute**: APC-specific notification protocol (may be used instead of NUT in some setups)

