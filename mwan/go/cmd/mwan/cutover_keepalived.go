package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"text/template"
)

//go:embed scripts/check_internet.sh.tmpl
var checkInternetTmpl string

//go:embed scripts/notify.sh.tmpl
var notifyTmpl string

// Unified keepalived config template. State and priority are passed as args.
const keepalivedConfTmpl = `vrrp_script chk_internet {
    script "/etc/keepalived/check_internet.sh"
    interval %d
    weight %d
    fall %d
    rise %d
}

vrrp_instance VI_HA {
    state %s
    interface %s
    virtual_router_id %d
    priority %d
    advert_int %d
    use_vmac vrrp.%d
    vmac_xmit_base
    virtual_ipaddress {
        %s
    }
    track_script {
        chk_internet
    }
    notify /etc/keepalived/notify.sh
}
`

// vrrpIface returns the keepalived vmac interface name for the configured VRID.
func vrrpIface(cfg *CutoverConfig) string {
	return fmt.Sprintf("vrrp.%d", cfg.VRID)
}

// renderKeepaliveConf renders the unified keepalived config for MASTER or BACKUP.
func renderKeepaliveConf(cfg *CutoverConfig, state string, iface string, priority int) string {
	return fmt.Sprintf(keepalivedConfTmpl,
		cfg.HealthCheckInterval, cfg.HealthCheckWeight, cfg.HealthCheckFall, cfg.HealthCheckRise,
		state, iface, cfg.VRID, priority, cfg.AdvertInterval, cfg.VRID,
		cfg.VIPIPv6)
}

// renderScript renders an embedded template with the given config.
func renderScript(tmplStr string, cfg *CutoverConfig) (string, error) {
	t, err := template.New("script").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// execFunc is a function that runs a shell command on a target.
type execFunc func(cmd string) (string, error)

// deployKeepalived deploys all keepalived scripts and config to a target via the given exec function.
// This is used by both migrate (SSH to VM) and start-backup (pct exec to LXC).
func deployKeepalived(ctx context.Context, log *slog.Logger, cfg *CutoverConfig, run execFunc, state string, iface string, priority int) error {
	// 1. Health check script
	log.Info("deploy-keepalived: writing health check script")
	checkScript, err := renderScript(checkInternetTmpl, cfg)
	if err != nil {
		return fmt.Errorf("render check_internet.sh: %w", err)
	}
	if _, err := run(fmt.Sprintf("mkdir -p /etc/keepalived && cat > /etc/keepalived/check_internet.sh << '__MWAN_EOF__'\n%s__MWAN_EOF__\nchmod +x /etc/keepalived/check_internet.sh", checkScript)); err != nil {
		return fmt.Errorf("write check_internet.sh: %w", err)
	}

	// 2. Notify script
	log.Info("deploy-keepalived: writing notify script")
	notifyScript, err := renderScript(notifyTmpl, cfg)
	if err != nil {
		return fmt.Errorf("render notify.sh: %w", err)
	}
	if _, err := run(fmt.Sprintf("cat > /etc/keepalived/notify.sh << '__MWAN_EOF__'\n%s__MWAN_EOF__\nchmod +x /etc/keepalived/notify.sh", notifyScript)); err != nil {
		return fmt.Errorf("write notify.sh: %w", err)
	}

	// 3. keepalived.conf
	log.Info("deploy-keepalived: writing keepalived.conf", "state", state, "priority", priority)
	conf := renderKeepaliveConf(cfg, state, iface, priority)
	if _, err := run(fmt.Sprintf("cat > /etc/keepalived/keepalived.conf << '__MWAN_EOF__'\n%s__MWAN_EOF__", conf)); err != nil {
		return fmt.Errorf("write keepalived.conf: %w", err)
	}

	return nil
}
