package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// EmailVerbosity controls how much detail is included in outbound alert emails.
// It does NOT suppress failure/rollback/vm-stopped alerts -- those always send.
// It only controls:
//   - Whether startup emails are sent at all
//   - How much body content is included in all emails
//
// Levels:
//
//	quiet   -- No startup email. Alert emails contain only the essential facts.
//	normal  -- Startup email with build version + config summary. Alert emails
//	           include probe history and channel health. (default)
//	verbose -- Startup email with full interface list and WAN config. Alert emails
//	           include everything normal does plus full host interface state.
type EmailVerbosity int

const (
	EmailVerbosityQuiet   EmailVerbosity = 0
	EmailVerbosityNormal  EmailVerbosity = 1
	EmailVerbosityVerbose EmailVerbosity = 2
)

func (v EmailVerbosity) String() string {
	switch v {
	case EmailVerbosityQuiet:
		return "quiet"
	case EmailVerbosityNormal:
		return "normal"
	case EmailVerbosityVerbose:
		return "verbose"
	default:
		return "unknown"
	}
}

// emailVerbosityFromEnv reads EMAIL_VERBOSITY and returns the corresponding
// level, defaulting to EmailVerbosityNormal if unset or unrecognized.
func emailVerbosityFromEnv() EmailVerbosity {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EMAIL_VERBOSITY"))) {
	case "quiet", "0":
		return EmailVerbosityQuiet
	case "verbose", "2":
		return EmailVerbosityVerbose
	default:
		return EmailVerbosityNormal
	}
}

// sendStartupEmail sends a "watchdog started" notification.  It is a no-op
// when verbosity is quiet or when the watchdog is in dry-run mode (dry-run
// wraps ops so sendEmail is a no-op there; we gate here to avoid the log
// noise of a skipped send).
func (w *watchdog) sendStartupEmail(ctx context.Context) {
	if w.cfg.EmailVerbosity == EmailVerbosityQuiet {
		w.log.Info("Startup email suppressed", "email_verbosity", w.cfg.EmailVerbosity)
		return
	}

	subject := fmt.Sprintf(
		"[mwan-watchdog] Started on %s (build %s)",
		w.cfg.PVENode,
		buildVersion(),
	)

	var b strings.Builder
	b.WriteString("mwan-watchdog has started.\n\n")

	b.WriteString("--- BUILD ---\n")
	b.WriteString(fmt.Sprintf("  %s\n\n", buildVersionString()))

	b.WriteString("--- CONFIG ---\n")
	b.WriteString(fmt.Sprintf(
		"  vmid=%s  pve_node=%s\n"+
			"  deploy_window_minutes=%d  connectivity_timeout_seconds=%d\n"+
			"  check_interval_healthy=%s  check_interval_degraded=%s\n"+
			"  alert_cooldown_seconds=%d  email_verbosity=%s\n\n",
		w.cfg.MwanVMID, w.cfg.PVENode,
		w.cfg.DeployWindowMinutes, w.cfg.ConnectivityTimeoutSeconds,
		w.cfg.CheckIntervalHealthy, w.cfg.CheckIntervalDegraded,
		w.cfg.AlertCooldownSeconds, w.cfg.EmailVerbosity,
	))

	b.WriteString("--- NETWORK CONFIG ---\n")
	b.WriteString(fmt.Sprintf(
		"  ping_target_ipv4=%s  ping_target_ipv6=%s\n"+
			"  wan_interfaces=%s\n"+
			"  last_deploy_path=%s\n\n",
		w.nc.PingTargetIPv4, w.nc.PingTargetIPv6,
		strings.Join(w.nc.wanIfaceNames(), ", "),
		w.nc.LastDeployPath,
	))

	b.WriteString("--- COMMUNICATION CHANNELS (startup state) ---\n")
	b.WriteString("  (channels are untested at startup; first probe will populate)\n\n")

	if w.cfg.EmailVerbosity >= EmailVerbosityVerbose {
		b.WriteString("--- HOST INTERFACE STATE ---\n")
		b.WriteString(buildSystemContext())
		b.WriteString("\n")
	}

	if len(w.cfg.ConfigWarnings) > 0 {
		b.WriteString("--- CONFIG WARNINGS ---\n")
		for _, cw := range w.cfg.ConfigWarnings {
			b.WriteString(fmt.Sprintf("  ! %s\n", cw))
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().Format(time.RFC3339)))

	if err := w.ops.sendEmail(ctx, w.cfg.AlertEmail, subject, b.String()); err != nil {
		w.log.Error("Failed to send startup email", "err", err, "to", w.cfg.AlertEmail)
	} else {
		w.log.Info("Startup email sent", "to", w.cfg.AlertEmail, "subject", subject)
	}
}
