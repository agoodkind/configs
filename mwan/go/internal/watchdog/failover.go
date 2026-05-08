package watchdog

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
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

	// Record failover state under the mutex so the run loop can later detect
	// recovery and emit the BGP RECOVERED email exactly once.
	w.failoverMu.Lock()
	w.failoverActive = true
	w.failoverStartedAt = w.now()
	w.failoverReason = reason
	w.failoverMu.Unlock()

	return nil
}

// triggerBGPRecovery moves routes back to the primary MWAN VM after it has
// returned to a healthy state. It mirrors triggerBGPFailover in shape:
// verify primary reachability, withdraw from the failover LXC, announce on
// the primary, send a recovery email, and clear failover state.
func (w *watchdog) triggerBGPRecovery(ctx context.Context, cfg *config.Config) error {
	start := w.now()

	w.failoverMu.Lock()
	startedAt := w.failoverStartedAt
	prevReason := w.failoverReason
	w.failoverMu.Unlock()

	// Step 1: confirm the primary VM is reachable. Reuse the existing host-
	// level Ping probes the watchdog already uses to gate failover decisions.
	w.log.InfoContext(ctx, "BGP_RECOVERY: probing primary VM reachability",
		"vmid", cfg.MwanVMID)
	v4ok := w.ops.Ping(ctx, "ping", cfg.Network.PingTargetIPv4)
	v6ok := w.ops.Ping(ctx, "ping6", cfg.Network.PingTargetIPv6)
	if !v4ok && !v6ok {
		return fmt.Errorf("BGP recovery: primary VM %s still unreachable", cfg.MwanVMID)
	}

	// Step 2: confirm primary's BGP session is established.
	w.log.InfoContext(ctx, "BGP_RECOVERY: checking BGP status on primary VM",
		"vmid", cfg.MwanVMID)
	status, err := w.ops.GetBGPStatus(ctx, cfg.MwanVMID)
	if err != nil {
		return fmt.Errorf("BGP recovery: GetBGPStatus on primary VM %s: %w", cfg.MwanVMID, err)
	}
	if status == nil || !status.GetAllEstablished() {
		return fmt.Errorf("BGP recovery: primary VM %s BGP not established", cfg.MwanVMID)
	}

	// Step 3: withdraw from the failover LXC.
	w.log.InfoContext(ctx, "BGP_RECOVERY: withdrawing routes from failover LXC",
		"lxc", cfg.Cutover.FailoverLXCID)
	if err := w.ops.WithdrawRoutes(ctx, cfg.Cutover.FailoverLXCID); err != nil {
		w.log.ErrorContext(ctx, "BGP_RECOVERY: withdraw routes failed on LXC",
			"lxc", cfg.Cutover.FailoverLXCID, "err", err)
		return fmt.Errorf("BGP recovery WithdrawRoutes on LXC %s: %w",
			cfg.Cutover.FailoverLXCID, err)
	}

	// Step 4: announce routes on the primary VM.
	w.log.InfoContext(ctx, "BGP_RECOVERY: announcing routes on primary VM",
		"vmid", cfg.MwanVMID)
	if err := w.ops.AnnounceRoutes(ctx, cfg.MwanVMID); err != nil {
		w.log.ErrorContext(ctx, "BGP_RECOVERY: announce routes failed on primary",
			"vmid", cfg.MwanVMID, "err", err)
		return fmt.Errorf("BGP recovery AnnounceRoutes on primary VM %s: %w",
			cfg.MwanVMID, err)
	}

	elapsedSinceFailover := time.Duration(0)
	if !startedAt.IsZero() {
		elapsedSinceFailover = w.now().Sub(startedAt).Round(time.Second)
	}

	subject := fmt.Sprintf("[%s] BGP RECOVERED: routes back on primary VM %s",
		cfg.Email.SubjectPrefix, cfg.MwanVMID)
	body := fmt.Sprintf(
		"BGP recovery completed in %s.\n\n"+
			"Original failover reason: %s\n"+
			"Time spent in failover: %s\n\n"+
			"Routes withdrawn from failover LXC %s.\n"+
			"Routes announced on primary VM %s.",
		w.since(start).Round(time.Second),
		prevReason,
		elapsedSinceFailover,
		cfg.Cutover.FailoverLXCID,
		cfg.MwanVMID,
	)
	_ = w.ops.SendEmail(ctx, cfg.Email.AlertEmail, subject, body)

	w.failoverMu.Lock()
	w.failoverActive = false
	w.failoverStartedAt = time.Time{}
	w.failoverReason = ""
	w.failoverMu.Unlock()

	w.log.InfoContext(ctx, "BGP_RECOVERY: complete",
		"vmid", cfg.MwanVMID,
		"elapsed", w.since(start),
	)

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

// maybeTriggerRecovery fires triggerBGPRecovery when the watchdog is currently
// in a failover state and the latest probe cycle is healthy. It is a no-op
// when failover is not active so it can be called unconditionally from the
// healthy branch of the run loop. Recovery state is cleared only on success
// inside triggerBGPRecovery, so a retry happens on the next healthy cycle if
// the primary's BGP session is not yet established.
func (w *watchdog) maybeTriggerRecovery(ctx context.Context, cfg *config.Config) {
	w.failoverMu.Lock()
	active := w.failoverActive
	w.failoverMu.Unlock()
	if !active {
		return
	}
	w.log.InfoContext(ctx, "BGP_RECOVERY: healthy cycle while failover active; attempting recovery")
	if err := w.triggerBGPRecovery(ctx, cfg); err != nil {
		w.log.WarnContext(ctx, "BGP_RECOVERY: deferred (will retry on next healthy cycle)",
			"err", err)
	}
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
		cfg:               cfg,
		ops:               ops.NewRealOps(cfg, emailSender, logger),
		coord:             &alert.Coord{},
		limiter:           alert.NewLimiter(cfg.Watchdog.AlertCooldownSeconds),
		log:               logger,
		runID:             runID,
		nowFn:             time.Now,
		failoverMu:        sync.Mutex{},
		failoverActive:    false,
		failoverStartedAt: time.Time{},
		failoverReason:    "",
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
