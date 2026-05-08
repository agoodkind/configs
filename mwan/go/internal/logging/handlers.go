package logging

import (
	"log/slog"
	"os"
	"time"

	"goodkind.io/gklog"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
)

// EmailFromConfig returns a slog.Handler that emails records at
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
	min := gklog.ParseLevel(cfg.Email.MinLevel)
	cd := 5 * time.Minute
	if cfg.Email.Cooldown != "" {
		if parsed, err := time.ParseDuration(cfg.Email.Cooldown); err == nil {
			cd = parsed
		}
	}
	subjectPrefix := ""
	if cfg.Email.SubjectPrefix != "" {
		subjectPrefix = cfg.Email.SubjectPrefix
	}
	// Use the project-local email handler so the body is rendered with
	// BuildEmailBody (tight What/Where/Trace/Build sections) instead of
	// gklog's flat key-value dump, which duplicated fields that
	// send-email already emits in the host-snapshot footer.
	return newEmailHandler(min, cd, sender, cfg.Email.AlertEmail, subjectPrefix)
}
