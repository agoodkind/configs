package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const backupKeepaliveConf = `vrrp_instance VI_HA {
    state BACKUP
    interface %s
    virtual_router_id %d
    priority %d
    advert_int %d
    use_vmac vrrp.%d
    vmac_xmit_base
    virtual_ipaddress {
        %s
        %s
    }
}
`

func cmdStartBackup(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	if cfg.DryRun {
		log.Info("start-backup: DRY RUN — would start keepalived BACKUP on LXC", "lxc", cfg.FailoverLXCID)
		return nil
	}

	lxc := cfg.FailoverLXCID

	lxcIface := cfg.FailoverLXCIface
	if lxcIface == "" {
		lxcIface = "eth1"
	}

	// Idempotency: if keepalived is already active in BACKUP state, skip
	if chkOut, chkErr := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"systemctl", "is-active", "keepalived"}, cfg.SSHTimeoutSec); chkErr == nil && strings.TrimSpace(chkOut) == "active" {
		if logOut, _ := localExec(ctx, "pct", []string{"exec", lxc, "--",
			"journalctl", "-u", "keepalived", "-n", "3", "--no-pager"}, cfg.SSHTimeoutSec); strings.Contains(logOut, "BACKUP") {
			log.Info("start-backup: already running in BACKUP state, skipping")
			return nil
		}
	}

	log.Info("start-backup: writing keepalived config on LXC", "lxc", lxc)
	conf := fmt.Sprintf(backupKeepaliveConf,
		lxcIface, cfg.VRID, cfg.BackupPriority, cfg.AdvertInterval, cfg.VRID,
		cfg.VIPIPv6, cfg.VIPIPv4)

	_, err := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"bash", "-c", fmt.Sprintf("cat > /etc/keepalived/keepalived.conf << 'KAEOF'\n%sKAEOF", conf)},
		cfg.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("write keepalived config on LXC %s: %w", lxc, err)
	}

	log.Info("start-backup: enabling and starting keepalived on LXC", "lxc", lxc)
	_, err = localExec(ctx, "pct", []string{"exec", lxc, "--",
		"systemctl", "enable", "--now", "keepalived"}, cfg.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("start keepalived on LXC %s: %w", lxc, err)
	}

	// Wait for BACKUP state
	log.Info("start-backup: waiting for BACKUP state")
	time.Sleep(3 * time.Second)

	out, err := localExec(ctx, "pct", []string{"exec", lxc, "--",
		"journalctl", "-u", "keepalived", "-n", "5", "--no-pager"}, cfg.SSHTimeoutSec)
	if err != nil {
		return fmt.Errorf("check keepalived state on LXC %s: %w", lxc, err)
	}

	if !strings.Contains(out, "BACKUP") {
		return fmt.Errorf("LXC %s did not enter BACKUP state:\n%s", lxc, out)
	}

	log.Info("start-backup: LXC is BACKUP", "lxc", lxc)
	return nil
}
