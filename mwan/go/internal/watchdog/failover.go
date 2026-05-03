package watchdog

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"goodkind.io/mwan/internal/alert"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
	"goodkind.io/mwan/internal/logging"
	"goodkind.io/mwan/internal/ops"
	"goodkind.io/mwan/internal/tracing"
	"goodkind.io/mwan/internal/version"
)

// triggerFailover dispatches to the BGP route-control failover path.
// The legacy keepalived-based path was removed when the BGP cutover
// completed (MWAN-1) and keepalived was decommissioned (MWAN-69 +
// MWAN-82). cfg.BGP.Enabled must be true on every host this watchdog
// runs against.
func (w *watchdog) triggerFailover(ctx context.Context, cfg *config.Config, reason string) error {
	if cfg.Cutover.FailoverLXCID == "" {
		return fmt.Errorf("cutover config has no failover_lxc_id; cannot failover")
	}
	if !cfg.BGP.Enabled {
		return fmt.Errorf("failover requires cfg.BGP.Enabled=true; legacy keepalived path was removed")
	}
	w.log.InfoContext(ctx, "FAILOVER: dispatching to BGP route control path", "reason", reason)
	return w.triggerBGPFailover(ctx, cfg, reason)
}

// triggerBGPFailover performs failover using BGP route control:
//  1. Withdraw routes from the primary MWAN VM
//  2. Check BGP status on the failover LXC
//  3. If the LXC is not yet announcing, tell it to announce
func (w *watchdog) triggerBGPFailover(ctx context.Context, cfg *config.Config, reason string) error {
	start := w.now()

	// Send alert email about BGP failover
	subject := fmt.Sprintf("[%s] BGP FAILOVER: withdrawing routes from VM, announcing on LXC %s",
		cfg.Email.SubjectPrefix, cfg.Cutover.FailoverLXCID)
	body := fmt.Sprintf(
		"Watchdog is triggering BGP failover.\n\nReason: %s\n\n"+
			"Step 1: Withdraw routes from primary VM %s\n"+
			"Step 2: Announce routes on failover LXC %s",
		reason, cfg.MwanVMID, cfg.Cutover.FailoverLXCID,
	)
	_ = w.ops.SendEmail(ctx, cfg.Email.AlertEmail, subject, body)

	// Step 1: Withdraw routes from primary VM.
	w.log.InfoContext(ctx, "BGP_FAILOVER: withdrawing routes from primary VM", "vmid", cfg.MwanVMID)
	if err := w.ops.WithdrawRoutes(ctx, cfg.MwanVMID); err != nil {
		w.log.ErrorContext(ctx, "BGP_FAILOVER: withdraw routes failed on primary", "vmid", cfg.MwanVMID, "err", err)
		// Continue because the primary may already be unreachable.
	} else {
		w.log.InfoContext(ctx, "BGP_FAILOVER: routes withdrawn from primary VM", "vmid", cfg.MwanVMID)
	}

	// Step 2: Check BGP status on the failover LXC.
	w.log.InfoContext(ctx, "BGP_FAILOVER: checking BGP status on failover LXC", "lxc", cfg.Cutover.FailoverLXCID)
	status, err := w.ops.GetBGPStatus(ctx, cfg.Cutover.FailoverLXCID)
	if err != nil {
		w.log.ErrorContext(ctx, "BGP_FAILOVER: failed to get BGP status on LXC", "lxc", cfg.Cutover.FailoverLXCID, "err", err)
		// Announce anyway because the LXC agent may still accept the command.
	}

	// Step 3: If LXC is not established or not already announcing, tell it to announce.
	needsAnnounce := true
	if status != nil && status.GetAllEstablished() && status.GetAnnouncing() {
		w.log.InfoContext(ctx, "BGP_FAILOVER: LXC already established and announcing; no action needed",
			"lxc", cfg.Cutover.FailoverLXCID)
		needsAnnounce = false
	}

	if needsAnnounce {
		w.log.InfoContext(ctx, "BGP_FAILOVER: announcing routes on failover LXC", "lxc", cfg.Cutover.FailoverLXCID)
		if err := w.ops.AnnounceRoutes(ctx, cfg.Cutover.FailoverLXCID); err != nil {
			w.log.ErrorContext(ctx, "BGP_FAILOVER: announce routes failed on LXC",
				"lxc", cfg.Cutover.FailoverLXCID, "err", err, "elapsed", w.since(start))
			return fmt.Errorf("BGP failover AnnounceRoutes on LXC %s: %w", cfg.Cutover.FailoverLXCID, err)
		}
		w.log.InfoContext(ctx, "BGP_FAILOVER: routes announced on failover LXC", "lxc", cfg.Cutover.FailoverLXCID)
	}

	w.log.InfoContext(ctx, "BGP_FAILOVER: complete",
		"lxc", cfg.Cutover.FailoverLXCID,
		"elapsed", w.since(start),
	)

	// Send success email
	successSubject := fmt.Sprintf("[%s] BGP FAILOVER COMPLETE: LXC %s announcing",
		cfg.Email.SubjectPrefix, cfg.Cutover.FailoverLXCID)
	successBody := fmt.Sprintf(
		"BGP failover completed in %s.\n\n"+
			"Routes withdrawn from primary VM %s.\n"+
			"Routes announced on failover LXC %s.",
		w.since(start).Round(time.Second), cfg.MwanVMID, cfg.Cutover.FailoverLXCID,
	)
	_ = w.ops.SendEmail(ctx, cfg.Email.AlertEmail, successSubject, successBody)

	return nil
}

// tryFailover attempts failover if the LXC has internet connectivity.
// Returns true if failover was triggered, false if skipped (LXC also down).
func (w *watchdog) tryFailover(ctx context.Context, cfg *config.Config, reason string) bool {
	if cfg.Cutover.FailoverLXCID == "" {
		w.log.WarnContext(ctx, "FAILOVER: no failover_lxc_id configured; skipping")
		return false
	}

	// Test LXC WAN connectivity before triggering failover.
	// If the LXC also has no internet, failover is pointless.
	w.log.InfoContext(ctx, "FAILOVER: testing LXC WAN connectivity", "lxc", cfg.Cutover.FailoverLXCID)
	lxcV4 := w.ops.Ping(ctx, "ping", cfg.Network.PingTargetIPv4)
	lxcV6 := w.ops.Ping(ctx, "ping6", cfg.Network.PingTargetIPv6)
	// TODO: ping from inside LXC specifically, not from host.
	// For now, host-level ping is a proxy.

	if !lxcV4 && !lxcV6 {
		w.log.WarnContext(ctx, "FAILOVER: LXC also has no internet; skipping failover (real ISP outage)",
			"lxc", cfg.Cutover.FailoverLXCID)
		return false
	}

	w.log.InfoContext(ctx, "FAILOVER: LXC has internet; proceeding",
		"lxc", cfg.Cutover.FailoverLXCID, "v4", lxcV4, "v6", lxcV6)

	if err := w.triggerFailover(ctx, cfg, reason); err != nil {
		w.log.ErrorContext(ctx, "FAILOVER: failed", "err", err)
		return false
	}
	return true
}

// FailoverRun is the entry point for `mwan watchdog failover`.
// It triggers failover immediately without the monitoring loop.
func FailoverRun(cfg *config.Config) error {
	handlers := []slog.Handler{logging.StdoutJSON()}
	if p := cfg.Watchdog.LogFile; p != "" {
		handlers = append(handlers, logging.FileText(p, "[watchdog]"))
	}
	if p := cfg.Watchdog.JSONLogFile; p != "" {
		handlers = append(handlers, logging.FileJSON(p))
	}
	if h := logging.EmailFromConfig(cfg, "mwan-watchdog"); h != nil {
		handlers = append(handlers, h)
	}
	logger, _ := logging.New(logging.Config{
		BuildVersion: version.BuildVersionString(),
		Handlers:     handlers,
	})
	runID := tracing.NewID()
	logger = logger.With(
		slog.String(tracing.RunIDKey, runID),
		slog.String(tracing.ComponentKey, "watchdog"),
	)

	// Create email sender
	emailSender := email.NewSender(cfg.Email.SMTP2GOAPIKey, cfg.Email.From, cfg.Email.BindIface, "mwan-watchdog", logger)

	// Create watchdog instance
	w := &watchdog{
		cfg:     cfg,
		ops:     ops.NewRealOps(cfg, emailSender, logger),
		coord:   &alert.Coord{},
		limiter: alert.NewLimiter(cfg.Watchdog.AlertCooldownSeconds),
		log:     logger,
		runID:   runID,
		nowFn:   time.Now,
	}

	ctx := context.Background()
	logger.InfoContext(ctx, "Manual failover requested")

	if err := w.triggerFailover(ctx, cfg, "manual trigger via `mwan watchdog failover`"); err != nil {
		logger.ErrorContext(ctx, "Failover failed", "err", err)
		return err
	}

	logger.InfoContext(ctx, "Failover complete")
	return nil
}
