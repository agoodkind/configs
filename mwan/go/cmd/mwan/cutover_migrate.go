package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const keepalivedConfTemplate = `vrrp_instance VI_HA {
    state MASTER
    interface %s
    virtual_router_id %d
    priority %d
    advert_int %d
    use_vmac vrrp.%d
    vmac_xmit_base
    virtual_ipaddress {
        %s
    }
    notify /etc/keepalived/notify.sh
}
`

// notifyScriptTemplate is deployed to both MASTER and BACKUP nodes.
// It handles: IPv4 VIP on vmac (keepalived VRRPv3 only adds IPv6),
// and OPNsense ARP+NDP cache flush on transitions.
const notifyScriptTemplate = `#!/bin/bash
# Deployed by mwan-cutover. Called by keepalived on state transitions.
# Args: $1=GROUP|INSTANCE $2=name $3=MASTER|BACKUP|FAULT

TYPE=$1
NAME=$2
STATE=$3

VIP_V4="%s"
VIP_V6="%s"
VMAC_IFACE="vrrp.%d"
OPNSENSE="%s"

log() { logger -t keepalived-notify "$STATE: $*"; echo "$(date) keepalived-notify $STATE: $*"; }

case $STATE in
    MASTER)
        log "Becoming MASTER"
        # Add IPv4 VIP to vmac (keepalived VRRPv3 only adds IPv6)
        ip addr replace $VIP_V4 dev $VMAC_IFACE 2>/dev/null
        log "Added IPv4 VIP $VIP_V4 to $VMAC_IFACE"
        # Flush OPNsense ARP+NDP so it re-resolves to the vmac MAC
        if [ -n "$OPNSENSE" ]; then
            V6=$(echo "$VIP_V6" | cut -d/ -f1)
            V4=$(echo "$VIP_V4" | cut -d/ -f1)
            ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 "$OPNSENSE" \
                "sudo ndp -d $V6; sudo arp -d $V4" 2>/dev/null
            log "Flushed OPNsense NDP+ARP for $V6 / $V4"
        fi
        ;;
    BACKUP)
        log "Becoming BACKUP"
        ;;
    FAULT)
        log "FAULT state"
        ;;
esac
`

func cmdMigrate(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	if cfg.DryRun {
		return migrateDryRun(ctx, log, cfg)
	}
	return migrateReal(ctx, log, cfg)
}

func migrateDryRun(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	log.Info("migrate: DRY RUN")
	log.Info("would add new real address", "addr", cfg.NewRealIPv6, "iface", cfg.MwanIntIface)
	log.Info("would write keepalived config and start keepalived")
	log.Info("would wait for VIP on vrrp.51")
	log.Info("would remove old real address", "addr", cfg.CurrentRealIPv6, "iface", cfg.MwanIntIface)
	return nil
}

func migrateReal(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	host := cfg.MwanMgmtAddr
	iface := cfg.MwanIntIface
	to := cfg.SSHTimeoutSec

	// Idempotency: if VIP is already on vrrp.51 and keepalived is MASTER, skip
	if chk, chkErr := sshExec(ctx, host, "ip -6 addr show dev vrrp.51 2>/dev/null", to); chkErr == nil {
		vipAddr := strings.Split(cfg.VIPIPv6, "/")[0]
		if strings.Contains(chk.Stdout, vipAddr) {
			if kaChk, _ := sshExec(ctx, host, "journalctl -u keepalived -n 3 --no-pager", to); strings.Contains(kaChk.Stdout, "MASTER") {
				log.Info("migrate: already migrated (VIP on vrrp.51, keepalived MASTER), skipping")
				return nil
			}
		}
	}

	// Step 1: Write notify script and keepalived config
	log.Info("migrate: writing notify script on VM")
	notifyScript := fmt.Sprintf(notifyScriptTemplate,
		cfg.VIPIPv4, cfg.VIPIPv6, cfg.VRID, cfg.OPNsenseAddr)
	_, err := sshMustExec(ctx, host,
		fmt.Sprintf("cat > /etc/keepalived/notify.sh << 'NSEOF'\n%sNSEOF\nchmod +x /etc/keepalived/notify.sh", notifyScript), to)
	if err != nil {
		return fmt.Errorf("write notify.sh: %w", err)
	}

	log.Info("migrate: writing keepalived.conf on VM")
	conf := fmt.Sprintf(keepalivedConfTemplate,
		iface, cfg.VRID, cfg.MasterPriority, cfg.AdvertInterval, cfg.VRID,
		cfg.VIPIPv6)

	_, err = sshMustExec(ctx, host,
		fmt.Sprintf("cat > /etc/keepalived/keepalived.conf << 'KAEOF'\n%sKAEOF", conf), to)
	if err != nil {
		return fmt.Errorf("write keepalived.conf: %w", err)
	}

	// Step 2: Add new real address (::3) alongside existing (::1)
	log.Info("migrate: adding new real address", "addr", cfg.NewRealIPv6)
	_, err = sshMustExec(ctx, host,
		fmt.Sprintf("ip -6 addr add %s dev %s nodad", cfg.NewRealIPv6, iface), to)
	if err != nil {
		return fmt.Errorf("add new real v6: %w", err)
	}
	_, err = sshMustExec(ctx, host,
		fmt.Sprintf("ip addr add %s dev %s", cfg.NewRealIPv4, iface), to)
	if err != nil {
		// Non-fatal: might already exist
		log.Warn("add new real v4 (may already exist)", "err", err)
	}

	// Step 3: Start keepalived (creates vrrp.51, adds VIP)
	log.Info("migrate: starting keepalived")
	_, err = sshMustExec(ctx, host, "systemctl start keepalived", to)
	if err != nil {
		return fmt.Errorf("start keepalived: %w", err)
	}

	// Step 4: Wait for VIP to appear on vrrp.51
	log.Info("migrate: waiting for VIP on vrrp.51")
	vipAddr := strings.Split(cfg.VIPIPv6, "/")[0]
	deadline := time.Now().Add(10 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		out, _ := sshExec(ctx, host, "ip -6 addr show dev vrrp.51 2>/dev/null", to)
		if strings.Contains(out.Stdout, vipAddr) {
			found = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !found {
		return fmt.Errorf("VIP %s did not appear on vrrp.51 within 10s", vipAddr)
	}
	log.Info("migrate: VIP confirmed on vrrp.51")

	// Step 4b: Wait for notify script to add IPv4 VIP
	log.Info("migrate: waiting for notify script to add IPv4 VIP")
	time.Sleep(2 * time.Second)
	v4Addr := strings.Split(cfg.VIPIPv4, "/")[0]
	v4Out, _ := sshExec(ctx, host, "ip -4 addr show dev vrrp.51 2>/dev/null", to)
	if !strings.Contains(v4Out.Stdout, v4Addr) {
		log.Warn("migrate: IPv4 VIP not on vrrp.51 after notify, adding manually")
		sshExec(ctx, host, fmt.Sprintf("ip addr replace %s dev vrrp.51", cfg.VIPIPv4), to)
	} else {
		log.Info("migrate: IPv4 VIP confirmed on vrrp.51 (added by notify script)")
	}

	// Step 5: Remove old real addresses from the physical interface
	log.Info("migrate: removing old real addresses from physical interface")
	if r, delErr := sshExec(ctx, host,
		fmt.Sprintf("ip -6 addr del %s dev %s", cfg.CurrentRealIPv6, iface), to); delErr != nil || r.ExitCode != 0 {
		log.Warn("migrate: failed to remove old IPv6 address (may already be gone)", "err", delErr, "stderr", r.Stderr)
	}
	if r, delErr := sshExec(ctx, host,
		fmt.Sprintf("ip addr del %s dev %s", cfg.CurrentRealIPv4, iface), to); delErr != nil || r.ExitCode != 0 {
		log.Warn("migrate: failed to remove old IPv4 address (may already be gone)", "err", delErr, "stderr", r.Stderr)
	}

	// Step 5b: Write deploy timestamp so the watchdog knows a deploy is in progress
	// and doesn't trigger rollback during the transition window
	log.Info("migrate: writing deploy timestamp to VM")
	_, _ = sshExec(ctx, host,
		fmt.Sprintf("date +%%s > /var/run/mwan-last-deploy"), to)

	// Step 6: Verify keepalived reached MASTER
	log.Info("migrate: verifying MASTER state")
	time.Sleep(2 * time.Second)
	out, err := sshMustExec(ctx, host,
		"journalctl -u keepalived -n 5 --no-pager", to)
	if err != nil {
		return fmt.Errorf("check keepalived state: %w", err)
	}
	if !strings.Contains(out, "MASTER") {
		return fmt.Errorf("keepalived did not reach MASTER state:\n%s", out)
	}
	log.Info("migrate: VM is MASTER, VIP migration complete")

	return nil
}
