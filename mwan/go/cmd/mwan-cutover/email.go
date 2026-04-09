package main

import (
	"context"
	"time"

	mailer "github.com/agoodkind/send-email/mailer"
)

// sendEmail sends via the send-email Go library with BindInterface set to
// mbrains, so the HTTPS request to SMTP2GO exits via Monkeybrains directly.
// This works even when the MWAN VIP is broken.
func sendEmail(cfg *CutoverConfig, subject, body string) error {
	if cfg.SMTP2GOAPIKey == "" {
		return nil
	}

	m := mailer.New(mailer.Config{
		SMTP2GOAPIKey:     cfg.SMTP2GOAPIKey,
		DefaultFromDomain: "goodkind.io",
		Transport:         mailer.MethodHTTP,
		BindInterface:     "mbrains",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return m.Send(ctx, mailer.Message{
		To:      cfg.AlertEmail,
		From:    cfg.EmailFrom,
		Subject: subject,
		Body:    body,
		Caller:  "mwan-cutover",
	})
}
