package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"
)

const (
	logPath     = "/var/log/mwan-cutover.log"
	emailTo     = "alex@goodkind.io"
	emailFrom   = "mwan-cutover@goodkind.io"
	subjectPfx  = "[MWAN-CUTOVER]"
	globalTO    = 5 * time.Minute
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	log := setupLogger()

	cfg, err := loadCutoverConfig()
	if err != nil {
		log.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	sub := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	var runErr error
	switch sub {
	case "preflight":
		runErr = cmdPreflight(ctx, log, cfg)
	case "migrate":
		runErr = cmdMigrate(ctx, log, cfg)
	case "start-backup":
		runErr = cmdStartBackup(ctx, log, cfg)
	case "verify":
		runErr = cmdVerify(ctx, log, cfg)
	case "rollback":
		runErr = cmdRollback(ctx, log, cfg)
	case "full":
		runErr = cmdFull(ctx, log, cfg)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", sub)
		usage()
		os.Exit(1)
	}

	if runErr != nil {
		log.Error("FAILED", "subcommand", sub, "err", runErr)
		_ = sendEmail(cfg, fmt.Sprintf("%s FAILED: %s", subjectPfx, sub),
			fmt.Sprintf("Subcommand %q failed at %s:\n\n%v", sub, time.Now().Format(time.RFC3339), runErr))
		os.Exit(1)
	}
	log.Info("completed", "subcommand", sub)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: mwan-cutover <subcommand> [flags]

subcommands:
  preflight      verify all preconditions, touch nothing
  migrate        atomic VIP migration on VM 113 (--dry-run available)
  start-backup   start keepalived on failover LXC
  verify         run end-to-end connectivity tests through VIP
  rollback       reverse VIP migration to pre-cutover state
  full           run all phases in sequence with verify after each`)
}

func cmdFull(ctx context.Context, log *slog.Logger, cfg *CutoverConfig) error {
	phases := []struct {
		name string
		fn   func(context.Context, *slog.Logger, *CutoverConfig) error
	}{
		{"preflight", cmdPreflight},
		{"migrate", cmdMigrate},
		{"start-backup", cmdStartBackup},
		{"verify", cmdVerify},
	}

	_ = sendEmail(cfg, fmt.Sprintf("%s STARTING full cutover", subjectPfx),
		fmt.Sprintf("Beginning HA cutover sequence at %s\n\nPhases: preflight, migrate, start-backup, verify",
			time.Now().Format(time.RFC3339)))

	for _, p := range phases {
		log.Info("=== PHASE START ===", "phase", p.name)
		_ = sendEmail(cfg, fmt.Sprintf("%s Phase: %s STARTING", subjectPfx, p.name),
			fmt.Sprintf("Starting phase %q at %s", p.name, time.Now().Format(time.RFC3339)))

		if err := p.fn(ctx, log, cfg); err != nil {
			_ = sendEmail(cfg, fmt.Sprintf("%s Phase: %s FAILED", subjectPfx, p.name),
				fmt.Sprintf("Phase %q failed at %s:\n\n%v\n\nRun: mwan-cutover rollback",
					p.name, time.Now().Format(time.RFC3339), err))
			return fmt.Errorf("phase %s: %w", p.name, err)
		}

		log.Info("=== PHASE COMPLETE ===", "phase", p.name)
		_ = sendEmail(cfg, fmt.Sprintf("%s Phase: %s OK", subjectPfx, p.name),
			fmt.Sprintf("Phase %q completed at %s", p.name, time.Now().Format(time.RFC3339)))
	}

	_ = sendEmail(cfg, fmt.Sprintf("%s CUTOVER COMPLETE", subjectPfx),
		fmt.Sprintf("All phases completed successfully at %s", time.Now().Format(time.RFC3339)))
	return nil
}
