# Troubleshooting Log

Quick reference for failures, edge cases, and fixes.

## How to Add Entries

When something breaks:

1. Add entry under relevant section (or create new section)
2. Use format:

   ```text
   ### YYYY-MM-DD: Brief description

   **Symptom**: What broke
   **Root cause**: Why it broke
   **Fix**: What fixed it
   **Prevention**: Config/code changes to prevent recurrence
   ```

---

## MWAN

### 2026-01-22: systemd-networkd restart broke internet + Cloudflare tunnel

**Symptom**:

- Deploy succeeded but internet went down after final `systemctl restart systemd-networkd`
- Cloudflare tunnel disconnected
- Complete external access lockout (only serial console worked)

**Root cause**:

1. Management interface had `DHCP=yes` → acquired duplicate IP (10.250.0.20) + duplicate routes
2. Management interface had `Gateway=3d06:bad:b01::1` → IPv6 traffic routed via mgmt instead of WAN
3. AT&T WAN IPv6 default route broken (connectivity issues)

**Fix**:

1. Removed DHCP IP: `networkctl reconfigure enmgmt0`
2. Deleted IPv4 mgmt default: `ip route del default via 10.250.0.1 dev enmgmt0`
3. Deleted IPv6 mgmt default: `ip -6 route del default via 3d06:bad:b01::1 dev enmgmt0`
4. Deleted broken AT&T IPv6 route: `ip -6 route del default via fe80::... dev enatt0.3242`
5. Restarted cloudflared: `systemctl restart cloudflared`

**Prevention**:

- Updated `ansible/templates/vm/10-mgmt.network.j2`:
  - `DHCP=no` (was `yes`)
  - `IPv6AcceptRA=no` (was `yes`)
  - Removed all `Gateway=` lines
- Management interface now only has static IPs for local access
- Internet traffic uses WAN interfaces only

**Files changed**:

- `/Users/agoodkind/Sites/configs/ansible/templates/vm/10-mgmt.network.j2`

### 2026-01-22: Watchdog didn't trigger auto-rollback

**Symptom**:

- Watchdog service failing with "null: unbound variable"
- No auto-rollback despite connectivity failure
- Restart counter at 135+ (crash loop)

**Root cause**:

1. File `/var/run/mwan-last-deploy` doesn't exist
2. `qm guest exec` returns JSON with `"out-data": null`
3. `jq -r '."out-data"'` returns string `"null"` (not empty)
4. Check `[[ -z "$deploy_ts" ]]` passes because `"null"` is not empty string
5. Line 48: `$(((now - null) / 60))` causes bash error "null: unbound variable"

**Fix**:
- Fixed timestamp parsing to handle "null" from jq
- Made watchdog always monitor (continuous mode)
- Added alert emails for non-deploy connectivity issues
- Tests from both Proxmox and inside MWAN VM

**Prevention**:
- Moved to env file pattern: `proxmox/config/mwan-watchdog.env.j2`
- Script sources env, service uses `EnvironmentFile`
- Created HTTP email script for reliable routing
- Updated post-deploy check to test from MWAN VM + cloudflared

---

## Outstanding Issues

### Watchdog doesn't monitor continuously [FIXED]

**Was**:
- Exits immediately if no recent deploy
- Crashes on missing/invalid timestamp file (`null: unbound variable`)
- No continuous health monitoring

**Now**:
- ✓ Always monitors connectivity (continuous mode)
- ✓ If broken + recent deploy → rollback
- ✓ If broken + no recent deploy → alert email
- ✓ Never crashes on missing file
- ✓ Tests from both Proxmox and inside MWAN VM

### No email notifications for events

**Current behavior**: Email only on rollback

**Required behavior**: Email on:

- Interface state changes (WAN up/down)
- Config changes
- Deploys (start/finish/fail)
- Startup/shutdown
- Health state transitions (healthy → unhealthy)

### Post-deploy connectivity check incomplete [FIXED]

**Was**: Tests from Proxmox to internet only

**Now**:
- ✓ Tests from MWAN VM itself (via qm guest exec)
- ✓ Tests IPv4 + IPv6 connectivity
- ✓ Tests cloudflared service status
- ✓ Only updates last-known-good if both pass

### Cloudflared no fallback route

**Current behavior**: Uses default routing (can fail if primary WAN down)

**Required behavior**: Fallback to monkeybrains if other WANs fail

---

## Ansible

### Example entry format

**Symptom**: Task fails with "variable undefined"

**Root cause**: `set_fact` referencing variable defined in same task

**Fix**: Split into sequential `set_fact` tasks

**Prevention**: Updated ansible-quality.mdc rule

---

## Template for New Entries

```markdown
### YYYY-MM-DD: Brief description

**Symptom**:
**Root cause**:
**Fix**:
**Prevention**:
**Files changed**:
```
