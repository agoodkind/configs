package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// sendEmail sends via SMTP2GO HTTP API, running the request as the
// cloudflared-oob user so it exits via Monkeybrains (UID-based routing).
// This ensures emails work even when the MWAN VIP is broken.
func sendEmail(cfg *CutoverConfig, subject, body string) error {
	if cfg.SMTP2GOAPIKey == "" {
		return nil
	}

	payload := map[string]interface{}{
		"api_key":   cfg.SMTP2GOAPIKey,
		"sender":    cfg.EmailFrom,
		"to":        []string{cfg.AlertEmail},
		"subject":   subject,
		"text_body": fmt.Sprintf("%s\n\n---\nHost: %s\nTime: %s", body, cfg.Hostname, time.Now().Format(time.RFC3339)),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal email: %w", err)
	}

	// Run curl as cloudflared-oob user so UID-based routing sends it via mbrains
	cmd := exec.Command("runuser", "-u", "cloudflared-oob", "--",
		"curl", "-sS", "--max-time", "10",
		"-X", "POST",
		"-H", "Content-Type: application/json",
		"-d", string(data),
		"https://api.smtp2go.com/v3/email/send")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("send email via oob: %w (output: %s)", err, string(out))
	}
	return nil
}
