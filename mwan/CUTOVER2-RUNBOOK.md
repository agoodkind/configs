# Cutover2 Testbed Runbook

Last updated: 2026-04-13. Keep this up to date as things change.

> **For post-cutover steady-state OPNsense config + operational rules** (e.g.
> "do not enable any v4/v6 gateway entity post-cutover or BGP default gets
> wiped on next Apply"), see [OPNSENSE-OPERATIONAL-NOTES.md](./OPNSENSE-OPERATIONAL-NOTES.md).

## Testbed topology

| Host | VMID | Role | Management address | Bridges |
|------|------|------|--------------------|---------|
| VM 950 | 950 | MWAN primary (mirrors prod VM 113) | `3d06:bad:b01:200::950` (vmbr1) | vmbr1 (mgmt), vmbr2 (internal/OPNsense), vmbr4/5/6 (ISP) |
| OPNsense 101 | 101 | Test gateway (mirrors prod router) | `192.168.1.1` (LAN/vmbr3), `10.250.250.2` (WAN/vmbr2) | vmbr2 (WAN), vmbr3 (LAN) |
| LXC 100 | 100 | Failover backup (mirrors prod LXC 116) | `3d06:bad:b01:200::100` (vmbr1) | vmbr1 (mgmt), vmbr2 (internal) |
| LXC 203 | 203 | LAN client (runs cutover2 API commands) | `3d06:bad:b01:211::x` (vmbr3) | vmbr3 (OPNsense LAN) |
| suburban | - | Hypervisor (runs gRPC commands) | `10.240.0.148` | all bridges |

## Where to run each command

Testbed splits cutover2 across two hosts because no single host can reach both OPNsense API (vmbr3) and agent gRPC (vmbr1).

| Command | Run from | Why |
|---------|----------|-----|
| `configure-opnsense` | LXC 203 | Needs OPNsense API at `192.168.1.1` (vmbr3) |
| `deploy-agents` | Hypervisor | Needs gRPC to agents via vsock/TCP (vmbr1) |
| `verify-coexistence` | Hypervisor | Needs gRPC to agents |
| `switch-to-bgp` | LXC 203 | Needs OPNsense API + SSH to OPNsense |
| `test-failover` | Hypervisor | Needs gRPC to agents |
| `unfuck` | Hypervisor | Needs `qm guest exec` + `pct exec` + OPNsense API |
| `rollback` | LXC 203 | Needs OPNsense API |

On production, vault has all paths (API + gRPC + qm/pct) so everything runs from vault via OOB (`root@3d06:bad:b01:ff::1`).

## Exact commands for each phase

### Prerequisites (after any snapshot rollback)

```bash
# 1. Re-run Ansible to deploy updated templates (nftables, scripts, etc.)
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-mwan-testbed.yml"
# NOTE: LXC 100 task will fail (SSH unreachable from workstation). That's OK.

# 2. Restart keepalived (snapshot rollback can leave VIP unassigned)
ssh root@10.240.0.148 "qm guest exec 950 -- bash -c 'systemctl restart keepalived'"

# 3. Restart MWAN services (NPT/routes need a kick after rollback)
ssh root@10.240.0.148 "qm guest exec 950 -- bash -c 'systemctl restart nftables mwan-update-npt mwan-update-routes'"

# 4. Verify connectivity
ssh root@10.240.0.148 "pct exec 203 -- ping -c 1 1.1.1.1"
ssh root@10.240.0.148 "pct exec 203 -- ping -c 1 2606:4700:4700::1111"

# 5. Deploy binary (stage on hypervisor first)
scp /Users/agoodkind/Sites/configs/mwan/go/bin/mwan-linux root@10.240.0.148:/tmp/mwan-linux

# VM 950
ssh root@10.240.0.148 "qm guest exec 950 -- bash -c 'systemctl stop mwan-agent; pkill mwan'"
ssh root@10.240.0.148 "scp /tmp/mwan-linux 'root@[3d06:bad:b01:200::950]:/usr/local/bin/mwan'"

# LXC 100
ssh root@10.240.0.148 "pct exec 100 -- systemctl stop mwan-agent"
ssh root@10.240.0.148 "pct push 100 /tmp/mwan-linux /usr/local/bin/mwan && pct exec 100 -- chmod +x /usr/local/bin/mwan"

# LXC 203
ssh root@10.240.0.148 "pct push 203 /tmp/mwan-linux /usr/local/bin/mwan && pct exec 203 -- chmod +x /usr/local/bin/mwan"
ssh root@10.240.0.148 "pct push 203 /etc/mwan/config.toml /etc/mwan/config.toml"

# Hypervisor binary already at /usr/local/bin/mwan from the scp above
ssh root@10.240.0.148 "cp /tmp/mwan-linux /usr/local/bin/mwan && chmod +x /usr/local/bin/mwan"
```

### Phase 1a: Configure OPNsense (from LXC 203)

```bash
ssh root@10.240.0.148 "pct exec 203 -- /usr/local/bin/mwan cutover2 configure-opnsense"
```

### Phase 1b: Start and verify agents (from hypervisor)

```bash
# Start agents
ssh root@10.240.0.148 "qm guest exec 950 -- bash -c 'systemctl start mwan-agent'"
ssh root@10.240.0.148 "pct exec 100 -- bash -c 'systemctl start mwan-agent'"

# Verify (wait ~15s for BGP to establish)
ssh root@10.240.0.148 "mwan cutover2 deploy-agents"
```

### Phase 1d: Verify coexistence (from hypervisor)

```bash
ssh root@10.240.0.148 "mwan cutover2 verify-coexistence"
```

### Phase 2: Switch to BGP (from LXC 203)

```bash
ssh root@10.240.0.148 "pct exec 203 -- /usr/local/bin/mwan cutover2 switch-to-bgp"
```

This will: force_down gateways, remove gatewayv6 from config.xml (via SSH), reboot OPNsense, wait for API + BGP.

### Verify post-cutover

```bash
ssh root@10.240.0.148 "pct exec 203 -- ping -c 3 1.1.1.1"
ssh root@10.240.0.148 "pct exec 203 -- ping -c 3 2606:4700:4700::1111"
```

### Rollback

```bash
# Soft rollback (re-enable gateways)
ssh root@10.240.0.148 "pct exec 203 -- /usr/local/bin/mwan cutover2 rollback"

# Nuclear rollback (snapshot)
ssh root@10.240.0.148 "qm rollback 950 pre-cutover2-reboot-clean && qm rollback 101 pre-cutover2-reboot-clean && qm start 950 && qm start 101"
```

## Snapshots

| Name | Description |
|------|-------------|
| `pre-cutover2-reboot-clean` | Ansible-deployed, .3 primary, keepalived MASTER .1 VIP, TCP 179 in nftables, LXC 203 SSH key on OPNsense. 2026-04-13 |

## Gotchas

1. **Never manually add .1 to enmwanbr0.** Keepalived manages VIP on vrrp.51. Manual .1 on enmwanbr0 causes GoBGP to source from .1, breaking IPv6 BGP (OPNsense expects .3).

2. **Keepalived FAULT log is cosmetic.** "entering FAULT state (no IPv6 address for interface)" is misleading. VIP still gets assigned. Check `ip addr show vrrp.51` to verify.

3. **Always re-run Ansible after snapshot rollback.** Snapshots predate template updates (nftables TCP 179 rule, etc.).

4. **`pct exec` PATH.** Does not include `/usr/local/bin`. Always use full path: `/usr/local/bin/mwan`.

5. **SSH to VM 950 needs boot time.** After `qm start 950`, wait ~30s before SSH. First attempt may fail with "Network is unreachable" (timing, not routing).

6. **LXC 203 SSH to OPNsense.** ed25519 key at `/root/.ssh/id_ed25519`, public key in OPNsense `/root/.ssh/authorized_keys`. Needed for gatewayv6 removal and reboot in switch-to-bgp.

7. **OPNsense didn't reboot.** In the 2026-04-13 test, SSH reboot failed (auth, but key was added after). BGP still worked because force_down removed static routes and BGP was already ESTABLISHED. The reboot is needed only for the IPv6 zebra stale cache issue (when gatewayv6 creates a competing static route). With force_down on both gateways, BGP may work without reboot.

8. **Pinging IPv6 from OPNsense shell needs `-s 16`.** FreeBSD `ping6` defaults to 8-byte payload. Webpass silently drops ICMPv6 with payload <= 8 bytes (confirmed 2026-04-24). OPNsense's `fe::2` mod-2 fwmark sends ~50% of pings via Webpass, producing fake "50% loss" results. Use `ping6 -s 16 <target>` for accurate diagnostics. Linux `ping -6` from LXC 203 / VM 113 is unaffected (default payload 56 bytes). See `memory/project_webpass_icmpv6_size.md`.

## Test results (2026-04-13)

### Run 1 (05:44 UTC): Partial success
- gatewayv6 removal FAILED (SSH auth, key not yet on OPNsense)
- OPNsense reboot FAILED (same auth)
- BGP took over anyway because force_down removed static routes
- All 4 peers ESTABLISHED, but IPv6 zebra stale cache issue untested (no reboot happened)

### Run 2 (06:05 UTC): Full success
- SSH key added to OPNsense, snapshot updated
- gatewayv6 removed from config.xml via SSH
- OPNsense reboot executed (SSH disconnect as expected)
- OPNsense came back after ~40s
- All 4 BGP peers ESTABLISHED, 1 prefix each (uptime 35s, fresh after reboot)
- IPv4: 3/3 pings, zero loss
- IPv6: 3/3 pings, zero loss
- **Reboot approach works end-to-end for both IPv4 and IPv6**

### Key finding
The OPNsense reboot clears zebra's stale cache completely. After reboot with gatewayv6 removed and gateways force_down, FRR starts clean, no static routes compete with BGP, both IPv4 and IPv6 BGP routes install in the kernel without any workarounds.

## Next steps (resume here)

1. Roll back to `pre-cutover2-reboot-clean` (both 950 and 101)
2. Start VMs, restart keepalived + nftables + NPT/routes on VM 950
3. Verify baseline connectivity from LXC 203
4. Run full e2e: configure-opnsense (LXC 203) -> start agents -> deploy-agents (hypervisor) -> switch-to-bgp (LXC 203)
5. Verify post-cutover IPv4 + IPv6 from LXC 203
6. This will be Run 4. Runs 2 and 3 passed (all 4 peers ESTABLISHED, connectivity verified). Run 3 had SSH failures that are now fixed with yq4 config.xml keys.
7. If Run 4 passes: run it again (Run 5) to prove reproducibility
8. Then test failover: kill VM 950 agent, verify LXC 100 takes over

## Snapshot inventory

| Name | VM | Description |
|------|----|-------------|
| `pre-cutover2-v2` | 950, 101 | Original pre-Ansible state. Missing TCP 179 nftables, no SSH key on OPNsense. DO NOT USE as starting point without re-running Ansible. |
| `pre-cutover2-reboot-clean` | 950 | Ansible-deployed, .3 primary, keepalived MASTER .1 VIP, TCP 179 in nftables. 2026-04-13. |
| `pre-cutover2-reboot-clean` | 101 | No FRR/BGP, disablereplyto=1, hypervisor+LXC203 SSH keys in config.xml (set via yq4, survives reboot). 2026-04-13. |

## OPNsense SSH key management

SSH keys for OPNsense MUST be in config.xml `<authorizedkeys>` (base64-encoded), NOT just `~/.ssh/authorized_keys`. OPNsense regenerates authorized_keys from config.xml on reboot and sshd restart. Keys added only to the file are lost.

To edit config.xml SSH keys, use mikefarah/yq (installed at `/usr/local/bin/yq4` on suburban):
```bash
# Download config
scp root@10.250.250.2:/conf/config.xml /tmp/config.xml

# Build combined base64 keys
COMBINED=$(printf '%s\n%s\n' "$(cat /root/.ssh/id_ed25519.pub)" "$(pct exec 203 -- cat /root/.ssh/id_ed25519.pub)")
B64=$(echo "$COMBINED" | base64 | tr -d '\n')

# Update with yq4
/usr/local/bin/yq4 -i -p xml -o xml ".opnsense.system.user.authorizedkeys = \"$B64\"" /tmp/config.xml

# Upload and reboot to apply
scp /tmp/config.xml root@10.250.250.2:/conf/config.xml
ssh root@10.250.250.2 'reboot'
```

NEVER use sed on config.xml. yq4 reformats self-closing tags (`<item />` to `<item></item>`) but OPNsense's PHP parser handles both forms.

The Python `yq` (v3.4.3, at `/usr/bin/yq`) does NOT support XML. Use `/usr/local/bin/yq4` (mikefarah v4.52.5).
