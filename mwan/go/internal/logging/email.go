package logging

import (
	"log/slog"
	"os"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/email"
)

// WithEmail attaches an email handler to a Config when the project's
// [email] section is populated. Inert (returns Config unchanged) when
// SMTP2GOAPIKey or AlertEmail is empty, so daemons can call this
// unconditionally.
//
// serviceName is used as the email "X-Mailer-style" identifier on the
// outgoing message, typically matching the systemd unit name (e.g.
// "mwan-watchdog", "mwan-agent").
func WithEmail(lc Config, cfg *config.Config, serviceName string) Config {
	if cfg.Email.SMTP2GOAPIKey == "" || cfg.Email.AlertEmail == "" {
		return lc
	}
	bootLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sender := email.NewSender(
		cfg.Email.SMTP2GOAPIKey,
		cfg.Email.From,
		cfg.Email.BindIface,
		serviceName,
		bootLogger,
	)
	lc.EmailSend = sender.Send
	lc.EmailTo = cfg.Email.AlertEmail
	lc.EmailMinLevel = cfg.Email.MinLevel
	if lc.EmailMinLevel == "" {
		lc.EmailMinLevel = "WARN"
	}
	lc.EmailCooldown = cfg.Email.Cooldown
	return lc
}
