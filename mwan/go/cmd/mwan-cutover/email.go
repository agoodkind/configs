package main

import (
	"context"
	"fmt"
	"time"

	mailer "github.com/agoodkind/send-email/mailer"
)

func sendEmail(cfg *CutoverConfig, subject, body string) error {
	if cfg.SMTP2GOAPIKey == "" {
		return nil
	}

	m := mailer.New(mailer.Config{
		SMTP2GOAPIKey:     cfg.SMTP2GOAPIKey,
		DefaultFromDomain: "goodkind.io",
		Transport:         mailer.MethodHTTP,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return m.Send(ctx, mailer.Message{
		To:      cfg.AlertEmail,
		From:    cfg.EmailFrom,
		Subject: subject,
		Body:    fmt.Sprintf("%s\n\n---\nHost: %s\nTime: %s", body, cfg.Hostname, time.Now().Format(time.RFC3339)),
		Caller:  "mwan-cutover",
	})
}
