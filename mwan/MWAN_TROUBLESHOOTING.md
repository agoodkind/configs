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

### 2026-01-22: systemd dependency cycles skipping critical services at boot

**Symptom**:

- Boot logs show "Ordering cycle found, skipping" for:
  - `paths.target`
  - `systemd-networkd.service`
  - `network-pre.target`
  - `network.target`
- wpa_supplicant-mwan.service and network stack start late or not at all

**Root cause (Cycle 1: wpa_supplicant-mwan)**:

1. `wpa-authenticated.path` had `After=wpa_supplicant-mwan.service`
2. Path units are part of `paths.target` which must complete before `basic.target`
3. But regular services need `basic.target` first
4. Cycle: wpa_supplicant-mwan → basic.target → paths.target → wpa-authenticated.path → wpa_supplicant-mwan

**Root cause (Cycle 2: nftables)**:

1. nftables override added `After=network-online.target`
2. Stock nftables has `Before=network-pre.target` (firewall before networking)
3. Cycle: nftables → network-pre.target → networkd → network.target → network-online.target → nftables

**Fix**:

1. `wpa-authenticated.path`: Removed `After=wpa_supplicant-mwan.service` (path units don't need ordering, they just watch files)
2. `wpa-authenticated.path`: Changed `WantedBy=multi-user.target` to `WantedBy=wpa_supplicant-mwan.service`
3. `wpa_supplicant.service`: Added `Wants=wpa-authenticated.path` to pull in the path
4. `nftables.service.d-override.conf`: Removed `After/Wants=network-online.target`

**Prevention**:

- Path units (`.path`) cannot have `After=` dependencies on regular services
- Never order nftables `After=` any network target when stock config has `Before=network-pre.target`

**Files changed**:

- `mwan/paths/wpa-authenticated.path`
- `mwan/services/wpa_supplicant.service`
- `mwan/overrides/nftables.service.d-override.conf`

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

### 2026-04-09: keepalived overwrites /etc/nftables.conf on suburban testbed VM 950

**Symptom**:

- After reboot, `nftables.service` fails: `Error: Interface does not exist` for `vrrp.51`
- All filter, NAT, NPT, and mangle rules missing
- VM 950 reachable on mgmt but all forwarding broken

**Root cause**:

1. During a previous session, runtime nftables state was saved with `nft list ruleset > /etc/nftables.conf`
2. This captured keepalived's `ip6 keepalived` table which references the `vrrp.51` macvlan interface
3. On boot, nftables loads before keepalived creates vrrp.51, causing the load to fail
4. Same class of issue as 2026-01-22 systemd dependency cycle: service ordering at boot

**Fix**:

1. Restored proper `/etc/nftables.conf` from repo (`mwan/testbed/vm-950/nftables.conf`)
2. NPT rules moved from runtime-only to static config (embedded in nftables.conf)
3. Keepalived adds its own table at runtime (harmless, coexists with static config)

**Prevention**:

- Never save runtime nftables state over `/etc/nftables.conf`
- NPT rules are now in the static config, not added at runtime
- If keepalived needs nft rules, they're added by keepalived itself after it creates vrrp.51

**Files changed**:

- `mwan/testbed/vm-950/nftables.conf`

### 2026-04-09: nftables locks out SSH when mgmt interface is eth0 not enmgmt0

**Symptom**:

- After deploying nftables config to VM 950, SSH times out
- VM running, all interfaces up, but no SSH access

**Root cause**:

1. Cloud-init names the management interface `eth0`
2. systemd-networkd link file renames it to `enmgmt0` (stable name)
3. Rename doesn't always happen (depends on boot order, cloud-init state)
4. nftables input chain only allows SSH on `enmgmt0`, drops on `eth0`
5. Same class as 2026-01-22: management interface naming assumptions

**Fix**:

- Changed nftables input rules to accept SSH on both `eth0` and `enmgmt0`:
  `iifname { $MGMT_IFACE, "eth0" } tcp dport 22 accept`

**Prevention**:

- Always allow management traffic on both possible interface names
- This mirrors production where the template handles interface naming via Jinja2

**Files changed**:

- `mwan/testbed/vm-950/nftables.conf`

### 2026-04-09: DAD conflict on enmwanbr0 when keepalived VIP active

**Symptom**:

- `ip addr show enmwanbr0` shows `3d06:bad:b01:201::1/64 scope global dadfailed tentative`
- Cannot ping OPNsense from VM 950 using GUA source address
- Same issue observed in production cutover attempts

**Root cause**:

1. Keepalived assigns `::1` as VIP on vrrp.51 (macvlan)
2. networkd also has `::1` configured on the physical enmwanbr0
3. Linux DAD detects the duplicate and marks enmwanbr0's copy as `dadfailed`
4. Outbound traffic from enmwanbr0 can't use `::1` as source

**Fix**:

- Remove `::1` from enmwanbr0 after keepalived starts
- Add `::3` as the "real" address on enmwanbr0 (post-cutover address per config)
- VIP `::1` lives only on vrrp.51

**Prevention**:

- The cutover tool's migrate phase handles this: assigns new real address, removes old
- networkd config for enmwanbr0 should use `::3` (post-cutover) not `::1` (VIP)

### 2026-04-10: ISP LXC emulation missing link/PD prefix separation

**Symptom**:

- LXC 100 (failover) gets SLAAC address from the PD prefix (`240::/60`)
- When masquerading, ISP LXC 202 routes replies via PD route to VM 950 (stopped) instead of back to LXC 100
- LXC 100's own IPv6 internet fails; IPv6 failover traffic not yet tested

**Root cause** (verified via tcpdump on ISP LXC 202):

- ISP LXC 202's radvd advertised `3d06:bad:b01:240::/60` for both SLAAC and PD delegation
- Real ISPs give SLAAC from a separate link prefix, not from the delegated prefix
- ISP LXC 202's PD route (`240::/60 via VM950 LL`) captured reply traffic meant for LXC 100's SLAAC address

**Partial fix applied** (LXC 100 own internet verified, failover E2E NOT yet verified):

- Added separate link `/64` (`3d06:bad:b01:250::/64`) to ISP LXC 202 for SLAAC
- radvd now advertises `250::/64` (AdvOnLink on, AdvAutonomous on)
- kea-dhcp6 delegates `240::/60` as PD (unchanged)
- Added kea-dhcp4 to ISP LXC 202 for DHCPv4 (was missing, LXC 100 had no IPv4 on WAN)
- LXC 100 gets SLAAC `250::` address and can reach internet directly (verified)
- Failover E2E (OPNsense -> LXC 100 -> ISP LXC -> internet) blocked by keepalived FAULT issue below

**Prevention**:

- ISP emulation must separate link prefix (SLAAC) from delegated prefix (PD)

### 2026-04-11: Keepalived on LXC 100 enters FAULT at boot, never recovers

**Symptom**:

- After LXC 100 reboot, keepalived enters FAULT state
- Log: `(VI_HA) entering FAULT state (no IPv6 address for interface)`
- Also logs: `(VI_HA) the first IPv6 VIP address should be link local`
- IPv6 VIP never assigned to vrrp.51
- IPv4 VIP present on vrrp.51 (from notify script or prior run)
- Manually restarting keepalived (`systemctl restart keepalived`) resolves it

**What has been verified**:

- Boot timing is NOT the cause. Added `After=networking.service` systemd override. `systemd-analyze critical-chain` confirms keepalived starts AFTER networking.service. eth1 has `201::4/64` when keepalived starts. FAULT still occurs.
- The FAULT occurs even when eth1 is UP with its IPv6 address assigned.
- The warning `"the first IPv6 VIP address should be link local"` appears on every boot but also appears on VM 950 where keepalived works fine. Unclear if this warning is causal or cosmetic.
- keepalived version 2.3.3 (includes fix for GitHub issue #2275).
- Production LXC 116 has the same architecture and the same risk but has never been cold-booted with keepalived enabled.

**What has NOT been verified**:

- The actual keepalived source code that produces `"no IPv6 address for interface"` has not been found. Attempts to search GitHub and fetch source files did not locate the exact line.
- Whether the FAULT is about the base interface (eth1) or the vmac interface (vrrp.51) is unknown.
- Whether adding a link-local VIP would fix it is speculative (not tested).
- Whether this is a keepalived bug, a Proxmox LXC veth behavior, or a configuration error is undetermined.

**Workaround**:

- `systemctl restart keepalived` after boot works reliably

**Status**: UNDER INVESTIGATION

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
