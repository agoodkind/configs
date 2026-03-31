package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/agoodkind/infra-tools/pkg/emaillog"
	"github.com/agoodkind/infra-tools/pkg/logging"
	mailer "github.com/agoodkind/send-email/mailer"
)

// mailerSender adapts send-email mailer to emaillog.Sender.
type mailerSender struct {
	cfg  mailer.Config
	from string
}

func (s *mailerSender) Send(
	ctx context.Context, to, subject, body string,
) error {
	m := mailer.New(s.cfg)
	return m.Send(ctx, mailer.Message{
		To:      to,
		From:    s.from,
		Subject: subject,
		Body:    body,
		Caller:  "mwan-watchdog",
	})
}

func newEmailSender(cfg config) emaillog.Sender {
	return &mailerSender{
		cfg: mailer.Config{
			SMTP2GOAPIKey:     cfg.SMTP2GOAPIKey,
			DefaultFromDomain: "goodkind.io",
			Transport:         mailer.MethodAuto,
		},
		from: emailSender,
	}
}

func parseEmailMinLevel(s string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}

// newWatchdogLogger tees to a text file, a JSON lines file, and JSON on stdout
// (journald). Paths come from cfg.LogFile and cfg.JSONLogFile.
// Every log record carries a "build" attribute with the git commit + binary hash
// so logs can always be correlated to an exact build, including uncommitted ones.
func newWatchdogLogger(cfg config) (*slog.Logger, error) {
	textLJ := logging.NewLumberjackWriter(cfg.LogFile)
	jsonLJ := logging.NewLumberjackWriter(cfg.JSONLogFile)

	txtH := logging.NewTextHandler(textLJ, "[mwan-watchdog]")
	jsonOpts := &slog.HandlerOptions{Level: slog.LevelDebug}
	jsonH := slog.NewJSONHandler(jsonLJ, jsonOpts)
	stdoutH := slog.NewJSONHandler(os.Stdout, jsonOpts)

	children := []slog.Handler{txtH, jsonH, stdoutH}
	if cfg.SMTP2GOAPIKey != "" && strings.TrimSpace(cfg.AlertEmail) != "" {
		threshold := parseEmailMinLevel(os.Getenv("EMAIL_MIN_LEVEL"))
		cdStr := getenv("EMAIL_COOLDOWN", "5m")
		cd, err := time.ParseDuration(cdStr)
		if err != nil {
			cd = 5 * time.Minute
		}
		emailH := emaillog.New(
			threshold, cd, newEmailSender(cfg), cfg.AlertEmail,
		)
		children = append(children, emailH)
	}

	logger := slog.New(logging.NewTeeHandler(children...)).
		With("build", buildVersion())
	return logger, nil
}
