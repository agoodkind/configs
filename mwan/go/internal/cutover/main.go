package cutover

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
	"goodkind.io/mwan/internal/logging"
	"goodkind.io/mwan/internal/tracing"
	"goodkind.io/mwan/internal/version"
)

const globalTO = 5 * time.Minute

// Run executes the cutover subcommand.
func Run(cfg *config.Config, dryRun bool) error {
	// Create logger
	log, lerr := logging.New(logging.WithEmail(logging.Config{JSONLogFile: "/var/log/mwan-cutover.log"}, cfg, "mwan-cutover"), version.BuildVersionString())
	if lerr != nil {
		return fmt.Errorf("logger init: %w", lerr)
	}
	runID := tracing.NewID()
	log = log.With(
		slog.String(tracing.RunIDKey, runID),
		slog.String(tracing.ComponentKey, "cutover"),
	)

	// Create email sender
	sender := email.NewSender(cfg.Email.SMTP2GOAPIKey, cfg.Email.From, cfg.Email.BindIface, "mwan-cutover", log)

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mwan cutover <preflight|migrate|start-backup|verify|rollback|full> [--dry-run]")
		return fmt.Errorf("missing subcommand")
	}
	subcommand := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	ctx, cancelTO := context.WithTimeout(ctx, globalTO)
	defer cancelTO()

	log.Info("discover: probing live state")
	if dErr := discoverRuntime(ctx, log, cfg); dErr != nil {
		log.Warn("discover: some values could not be discovered (will use TOML values)", "err", dErr)
	}

	var runErr error
	switch subcommand {
	case "preflight":
		runErr = cmdPreflight(ctx, log, cfg)
	case "migrate":
		runErr = cmdMigrate(ctx, log, cfg, dryRun)
	case "start-backup":
		runErr = cmdStartBackup(ctx, log, cfg, dryRun)
	case "verify":
		runErr = cmdVerify(ctx, log, cfg, dryRun)
	case "rollback":
		runErr = cmdRollback(ctx, log, cfg)
	case "full":
		runErr = cmdFull(ctx, log, cfg, sender, dryRun)
	default:
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}

	if runErr != nil {
		log.Error("FAILED", "subcommand", subcommand, "err", runErr)
		_ = sender.Send(ctx, cfg.Email.AlertEmail, fmt.Sprintf("%s FAILED: %s", cfg.Email.SubjectPrefix, subcommand),
			fmt.Sprintf("Subcommand %q failed at %s:\n\n%v", subcommand, time.Now().Format(time.RFC3339), runErr))
		return runErr
	}
	log.Info("completed", "subcommand", subcommand)
	return nil
}

func cmdFull(ctx context.Context, log *slog.Logger, cfg *config.Config, sender *email.Sender, dryRun bool) error {
	phases := []struct {
		name string
		fn   func(context.Context, *slog.Logger, *config.Config, bool) error
	}{
		{"preflight", func(ctx context.Context, log *slog.Logger, cfg *config.Config, dryRun bool) error {
			return cmdPreflight(ctx, log, cfg)
		}},
		{"migrate", cmdMigrate},
		{"start-backup", cmdStartBackup},
		{"verify", cmdVerify},
	}

	if dryRun {
		log.Info("full: DRY RUN — preflight runs for real, everything else is simulated")
	}

	mode := "LIVE"
	if dryRun {
		mode = "DRY RUN"
	}
	if sender != nil {
		_ = sender.Send(ctx, cfg.Email.AlertEmail, fmt.Sprintf("%s STARTING full cutover (%s)", cfg.Email.SubjectPrefix, mode),
			fmt.Sprintf("Beginning HA cutover sequence (%s) at %s\n\nPhases: preflight, migrate, start-backup, verify",
				mode, time.Now().Format(time.RFC3339)))
	}

	for _, p := range phases {
		log.Debug("=== PHASE START ===", "phase", p.name)
		if sender != nil {
			_ = sender.Send(ctx, cfg.Email.AlertEmail, fmt.Sprintf("%s Phase: %s STARTING", cfg.Email.SubjectPrefix, p.name),
				fmt.Sprintf("Starting phase %q at %s", p.name, time.Now().Format(time.RFC3339)))
		}

		if err := p.fn(ctx, log, cfg, dryRun); err != nil {
			if sender != nil {
				_ = sender.Send(ctx, cfg.Email.AlertEmail, fmt.Sprintf("%s Phase: %s FAILED", cfg.Email.SubjectPrefix, p.name),
					fmt.Sprintf("Phase %q failed at %s:\n\n%v\n\nRun: mwan-cutover rollback",
						p.name, time.Now().Format(time.RFC3339), err))
			}
			return fmt.Errorf("phase %s: %w", p.name, err)
		}

		log.Debug("=== PHASE COMPLETE ===", "phase", p.name)
		if sender != nil {
			_ = sender.Send(ctx, cfg.Email.AlertEmail, fmt.Sprintf("%s Phase: %s OK", cfg.Email.SubjectPrefix, p.name),
				fmt.Sprintf("Phase %q completed at %s", p.name, time.Now().Format(time.RFC3339)))
		}
	}

	if sender != nil {
		_ = sender.Send(ctx, cfg.Email.AlertEmail, fmt.Sprintf("%s CUTOVER COMPLETE", cfg.Email.SubjectPrefix),
			fmt.Sprintf("All phases completed successfully at %s", time.Now().Format(time.RFC3339)))
	}
	return nil
}
