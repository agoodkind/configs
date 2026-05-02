package logging

import (
	"log/slog"
	"strings"

	"goodkind.io/mwan/pkg/logging"
)

// Config bundles the slog handlers a daemon wants composed into its
// logger plus the build-version annotation that should accompany every
// record. Project callers compose Handlers via the StdoutJSON / FileText
// / FileJSON / EmailFromConfig constructors in this package.
type Config struct {
	BuildVersion string
	Handlers     []slog.Handler
}

// New composes Handlers via TeeHandler under the project's
// ContextHandler and returns a slog.Logger pre-annotated with the
// build version. Returns a usable logger even when Handlers is empty
// (records go nowhere; intended for tests).
func New(cfg Config) *slog.Logger {
	return slog.New(NewContextHandler(logging.NewTeeHandler(cfg.Handlers...))).
		With("build", cfg.BuildVersion)
}

// ParseEmailMinLevel converts a string ("DEBUG", "INFO", "WARN",
// "ERROR") to slog.Level. Defaults to LevelWarn for empty or
// unrecognised input. Exposed because cfg.Email.MinLevel is a string in
// TOML and several callsites need to apply the same parsing.
func ParseEmailMinLevel(s string) slog.Level {
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
