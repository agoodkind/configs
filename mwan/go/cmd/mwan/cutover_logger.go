package main

import (
	"io"
	"log/slog"
	"os"
)

func cutoverSetupLogger() *slog.Logger {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		// Fall back to stdout only
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	w := io.MultiWriter(os.Stdout, f)
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
