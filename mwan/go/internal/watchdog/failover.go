package watchdog

import (
	"context"
	"fmt"
	"time"

	"goodkind.io/mwan/internal/alert"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/cutover"
	"goodkind.io/mwan/internal/email"
	"goodkind.io/mwan/internal/logging"
	"goodkind.io/mwan/internal/ops"
	"goodkind.io/mwan/internal/version"
)

// triggerFailover activates the failover LXC as a hot standby.
// It loads the cutover config and calls cutover.StartBackup which:
//   - Configures forwarding, masquerade, routes on the LXC
//   - Deploys and starts keepalived in BACKUP state
//   - keepalived's health check on the primary will detect the outage
//     and promote the LXC to MASTER automatically
//
// If the primary VM's keepalived health check is already failing,
// the LXC will promote to MASTER within a few seconds of starting.
func (w *watchdog) triggerFailover(ctx context.Context, cfg *config.Config, reason string) error {
	if cfg.Cutover.FailoverLXCID == "" {
		return fmt.Errorf("cutover config has no failover_lxc_id; cannot failover")
	}

	w.log.Info("FAILOVER: activating backup LXC",
		"lxc", cfg.Cutover.FailoverLXCID,
		"reason", reason,
	)

	// Send alert email about failover
	subject := fmt.Sprintf("[%s] FAILOVER: activating LXC %s", cfg.Email.SubjectPrefix, cfg.Cutover.FailoverLXCID)
	body := fmt.Sprintf(
		"Watchdog is triggering failover to LXC %s.\n\nReason: %s\n\nThe backup LXC will be configured and keepalived will promote it to MASTER.",
		cfg.Cutover.FailoverLXCID, reason,
	)
	w.ops.SendEmail(ctx, cfg.Email.AlertEmail, subject, body)

	start := time.Now()
	if err := cutover.StartBackup(ctx, w.log, cfg); err != nil {
		w.log.Error("FAILOVER: StartBackup failed", "err", err, "elapsed", time.Since(start))
		return fmt.Errorf("failover StartBackup: %w", err)
	}

	w.log.Info("FAILOVER: backup LXC activated successfully",
		"lxc", cfg.Cutover.FailoverLXCID,
		"elapsed", time.Since(start),
	)

	// Send success email
	successSubject := fmt.Sprintf("[%s] FAILOVER COMPLETE: LXC %s active", cfg.Email.SubjectPrefix, cfg.Cutover.FailoverLXCID)
	successBody := fmt.Sprintf(
		"Failover to LXC %s completed in %s.\n\nKeepalived should promote the LXC to MASTER shortly.",
		cfg.Cutover.FailoverLXCID, time.Since(start).Round(time.Second),
	)
	w.ops.SendEmail(ctx, cfg.Email.AlertEmail, successSubject, successBody)

	return nil
}

// tryFailover attempts failover if the LXC has internet connectivity.
// Returns true if failover was triggered, false if skipped (LXC also down).
func (w *watchdog) tryFailover(ctx context.Context, cfg *config.Config, reason string) bool {
	if cfg.Cutover.FailoverLXCID == "" {
		w.log.Warn("FAILOVER: no failover_lxc_id configured; skipping")
		return false
	}

	// Test LXC WAN connectivity before triggering failover.
	// If the LXC also has no internet, failover is pointless.
	w.log.Info("FAILOVER: testing LXC WAN connectivity", "lxc", cfg.Cutover.FailoverLXCID)
	lxcV4 := w.ops.Ping(ctx, "ping", cfg.Network.PingTargetIPv4)
	lxcV6 := w.ops.Ping(ctx, "ping6", cfg.Network.PingTargetIPv6)
	// TODO: ping from inside LXC specifically, not from host.
	// For now, host-level ping is a proxy.

	if !lxcV4 && !lxcV6 {
		w.log.Warn("FAILOVER: LXC also has no internet; skipping failover (real ISP outage)",
			"lxc", cfg.Cutover.FailoverLXCID)
		return false
	}

	w.log.Info("FAILOVER: LXC has internet; proceeding",
		"lxc", cfg.Cutover.FailoverLXCID, "v4", lxcV4, "v6", lxcV6)

	if err := w.triggerFailover(ctx, cfg, reason); err != nil {
		w.log.Error("FAILOVER: failed", "err", err)
		return false
	}
	return true
}

// FailoverRun is the entry point for `mwan watchdog failover`.
// It triggers failover immediately without the monitoring loop.
func FailoverRun(cfg *config.Config) error {
	// Create logger
	logger, err := logging.New(logging.Config{
		TextLogFile: cfg.Watchdog.LogFile,
		JSONLogFile: cfg.Watchdog.JSONLogFile,
	}, version.BuildVersionString())
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}

	// Create email sender
	emailSender := email.NewSender(cfg.Email.SMTP2GOAPIKey, cfg.Email.From, cfg.Email.BindIface, "mwan-watchdog", logger)

	// Create watchdog instance
	w := &watchdog{
		cfg:     cfg,
		ops:     ops.NewRealOps(cfg, emailSender),
		coord:   &alert.Coord{},
		limiter: alert.NewLimiter(cfg.Watchdog.AlertCooldownSeconds),
		log:     logger,
	}

	ctx := context.Background()
	logger.Info("Manual failover requested")

	if err := w.triggerFailover(ctx, cfg, "manual trigger via `mwan watchdog failover`"); err != nil {
		logger.Error("Failover failed", "err", err)
		return err
	}

	logger.Info("Failover complete")
	return nil
}
