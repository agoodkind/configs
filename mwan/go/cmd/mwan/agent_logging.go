package main

import (
	"log/slog"
	"os"

	"github.com/agoodkind/infra-tools/pkg/logging"
)

// newAgentLogger tees to a text file and JSON on stdout (e.g. journald).
// Every log record carries a "build" attribute with the git commit + binary hash.
func newAgentLogger(logFile string) (*slog.Logger, error) {
	textLJ := logging.NewLumberjackWriter(logFile)
	txtH := logging.NewTextHandler(textLJ, "[mwan-agent]")
	jsonOpts := &slog.HandlerOptions{Level: slog.LevelDebug}
	stdoutH := slog.NewJSONHandler(os.Stdout, jsonOpts)

	logger := slog.New(logging.NewTeeHandler(txtH, stdoutH)).
		With("build", buildVersion())
	return logger, nil
}
