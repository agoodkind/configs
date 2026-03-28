---
name: Rollback test and reboot
overview: Live rollback test on VM 200 using the Go watchdog's red-team mode, followed by a scheduled safe reboot of vault at 2am PDT to verify the full stack recovers cleanly.
todos:
  - id: patch-redteam
    content: Change InjectSnapshot to false in total-loss-mwan preset in redteam.go
    status: pending
  - id: rebuild-deploy
    content: Rebuild mwan-watchdog binary and deploy to vault
    status: completed
  - id: prep-vm200
    content: "Prepare VM 200: start, install qemu-guest-agent, write deploy timestamp, snapshot pre-deploy-test, poweroff"
    status: completed
  - id: run-rollback-test
    content: Run red-team total-loss-mwan test against VM 200, verify rollback log sequence
    status: completed
  - id: verify-live-watchdog
    content: Verify live mwan-watchdog for VM 113 still healthy after test
    status: completed
  - id: schedule-reboot
    content: Schedule vault reboot at 2am PDT via at command
    status: pending
  - id: monitor-reboot
    content: Monitor vault and VM 113 recovery after reboot
    status: pending
isProject: false
---

# Rollback Test and Vault Reboot

## Phase 1: Live rollback test on VM 200

### Setup VM 200 with real state

VM 200 is a stopped Debian cloud-init VM on `vmbr0` with `agent: enabled=1`. We need it to have:
- `qemu-guest-agent` running (so `qm guest exec` works -- the watchdog uses this for ISP per-interface probes)
- A real `pre-deploy-*` snapshot
- A real deploy timestamp at `/var/run/mwan-last-deploy` inside the guest

Steps:
1. `qm start 200` -- wait for boot (~30s)
2. `qm guest exec 200 -- apt-get install -y qemu-guest-agent` + `systemctl start qemu-guest-agent`
3. Write deploy timestamp: `qm guest exec 200 -- bash -c "echo $(date +%s) > /var/run/mwan-last-deploy"`
4. Take snapshot: `qm snapshot 200 pre-deploy-test --description "Test rollback target"`
5. Shut down guest OS cleanly: `qm guest exec 200 -- poweroff` (watchdog will restart it)

### Run the test

Run the Go binary directly on vault with `MWAN_VMID=200` and a separate log/state file so it doesn't interfere with the live watchdog:

```bash
MWAN_VMID=200 \
LOG_FILE=/tmp/watchdog-test.log \
ROLLBACK_STATE_FILE=/tmp/watchdog-test.state \
SMTP2GO_API_KEY=api-2F941C28446E4437B7F0F52EECCAD69E \
/usr/local/bin/mwan-watchdog \
  --red-team total-loss-mwan \
  --red-team-iterations 15 \
  2>&1 | tee /tmp/watchdog-test-stdout.log
```

`total-loss-mwan` fakes host connectivity loss, VM default-route failure, and injects a deploy timestamp -- but uses a **real snapshot**. The preset has `InjectSnapshot: true` which generates a fake snapshot name that won't exist. To use the real `pre-deploy-test` snapshot, we need to run without `InjectSnapshot` -- meaning use real state in the VM instead.

**Adjusted approach**: use `--red-team proxmox-routing` won't rollback. Instead, run without red-team but with a real broken state: start VM 200, don't install any routing, let connectivity probes fail naturally. But this requires real network isolation which is harder.

**Simplest working approach**: patch `total-loss-mwan` preset to set `InjectSnapshot: false` so it uses `r.inner.vmSnapshots()` which reads the real `pre-deploy-test` snapshot from VM 200. This is a one-line code change.

File: [`mwan/go/cmd/mwan-watchdog/redteam.go`](mwan/go/cmd/mwan-watchdog/redteam.go) -- change `InjectSnapshot: true` to `InjectSnapshot: false` in `total-loss-mwan`.

Rebuild and deploy to vault, then run the test command above.

### Expected sequence

```
[red-team] total-loss-mwan: Both fail, ISP up -> MWAN routing failure -> rollback
[mwan-watchdog] Connectivity TOTAL LOSS (was healthy)
[mwan-watchdog] VM 200: default-route ping FAILED (red-team injected)
[mwan-watchdog] ISP reachable per-interface; MWAN routing failure
[mwan-watchdog] Recent deploy found (ts=...)
[mwan-watchdog] Pre-deploy snapshot: pre-deploy-test
[mwan-watchdog] Rolling back VM 200 to pre-deploy-test
[mwan-watchdog] VM 200 stopped
[mwan-watchdog] VM 200 rolled back
[mwan-watchdog] VM 200 started
[mwan-watchdog] Rollback complete; entering grace period
```

After the test, take another snapshot of VM 200 for future use and verify the live watchdog for VM 113 is still running healthy.

---

## Phase 2: Vault reboot at 2am PDT

No config changes. Pure safety check -- confirm vault, VM 113, and all services come back cleanly.

Schedule via `at`:

```bash
echo "systemctl reboot" | at 02:00
```

### Expected recovery sequence (takes ~2-3 min total)

1. vault shuts down all VMs cleanly via `qm shutdown` (QEMU ACPI shutdown, ~30s per VM)
2. vault reboots (~30s kernel boot)
3. Proxmox starts VMs with `onboot: 1` -- OPNsense (101) first, then mwan (113) and others
4. VM 113 boots with PCIe passthrough NICs, starts cloudflared, wpa_supplicant, nftables
5. `mwan-watchdog.service` starts on vault, detects connectivity healthy within first probe cycle

### What to watch

- After reboot, check `journalctl -u mwan-watchdog -f` for first healthy log line
- Check `qm status 113` is running
- Check `ping6 2606:4700:4700::1111` from vault

### Rollback safety during reboot window

The watchdog is not running during the vault reboot itself. VM 113 will be down for ~2-3 min. If something goes wrong with VM 113's boot (e.g. PCIe device assignment fails), the watchdog will start on vault and see connectivity failure -- but it won't rollback unless it also sees a recent deploy timestamp in the guest (which it won't, since no deploy happened). So the worst case is an email alert, not an unintended rollback.

---

## Implementation order

1. Change `InjectSnapshot: false` in `redteam.go`
2. Rebuild Go binary, copy to vault
3. Set up VM 200 (start, install agent, snapshot, write timestamp, poweroff)
4. Run red-team test, observe and verify log
5. Verify live watchdog for VM 113 still healthy
6. Schedule vault reboot: `echo "systemctl reboot" | at 02:00`
7. Monitor recovery after 2am
