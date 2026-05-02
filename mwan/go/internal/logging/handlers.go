package logging

import (
	"log/slog"
	"os"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
	"goodkind.io/mwan/pkg/emaillog"
	"goodkind.io/mwan/pkg/logging"
)

// StdoutJSON returns a slog handler that writes JSON records to stdout
// at LevelDebug or above. Intended for journald capture; the
// systemd-journald daemon classifies records by their level field.
func StdoutJSON() slog.Handler {
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
}

// FileText returns a text handler that writes a human-friendly format
// (matching the project's [mwan]-style prefix) to a lumberjack-rotated
// file at path. label is the bracketed prefix on every line, e.g.
// "[watchdog]" or "[mwan]".
func FileText(path, label string) slog.Handler {
	return logging.NewTextHandler(logging.NewLumberjackWriter(path), label)
}

// FileJSON returns a JSON handler that writes records at LevelDebug or
// above to a lumberjack-rotated file at path.
func FileJSON(path string) slog.Handler {
	return slog.NewJSONHandler(
		logging.NewLumberjackWriter(path),
		&slog.HandlerOptions{Level: slog.LevelDebug},
	)
}

// EmailFromConfig returns an email handler that sends records at
// cfg.Email.MinLevel (default WARN) or above to cfg.Email.AlertEmail
// via SMTP2GO, with a per-subject cooldown to avoid spam. serviceName
// is the X-Mailer-style identifier on outgoing messages, typically the
// systemd unit name (e.g. "mwan-watchdog", "mwan-agent").
//
// Returns nil when SMTP2GOAPIKey or AlertEmail is empty so callers can
// just append the result and skip the nil:
//
//	handlers := []slog.Handler{logging.StdoutJSON()}
//	if h := logging.EmailFromConfig(cfg, "mwan-agent"); h != nil {
//	    handlers = append(handlers, h)
//	}
func EmailFromConfig(cfg *config.Config, serviceName string) slog.Handler {
	if cfg.Email.SMTP2GOAPIKey == "" || cfg.Email.AlertEmail == "" {
		return nil
	}
	bootLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sender := email.NewSender(
		cfg.Email.SMTP2GOAPIKey,
		cfg.Email.From,
		cfg.Email.BindIface,
		serviceName,
		bootLogger,
	)
	min := ParseEmailMinLevel(cfg.Email.MinLevel)
	cd := 5 * time.Minute
	if cfg.Email.Cooldown != "" {
		if parsed, err := time.ParseDuration(cfg.Email.Cooldown); err == nil {
			cd = parsed
		}
	}
	return emaillog.New(min, cd, sender, cfg.Email.AlertEmail)
}
