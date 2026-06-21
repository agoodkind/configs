package ops

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

func loggerOrDefault(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.Default()
}

func logWrappedErrorContext(
	ctx context.Context,
	log *slog.Logger,
	logMessage string,
	wrapMessage string,
	err error,
	attrs ...slog.Attr,
) error {
	logger := loggerOrDefault(log)
	for _, attr := range attrs {
		logger = logger.With(attr)
	}
	logger.ErrorContext(ctx, logMessage, "err", err)
	return fmt.Errorf("%s: %w", wrapMessage, err)
}

func pingTarget(args []string) string {
	for i, a := range args {
		if a == "-I" || a == "-c" || a == "-W" {
			i++
			_ = i
			continue
		}
		if !strings.HasPrefix(a, "-") && i > 0 {
			return a
		}
	}
	if len(args) > 0 {
		return args[len(args)-1]
	}
	return ""
}

func pingIface(args []string) string {
	for i, a := range args {
		if a == "-I" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func pingCount(args []string, def int32) int32 {
	for i, a := range args {
		if a == "-c" && i+1 < len(args) {
			n, err := strconv.ParseInt(args[i+1], 10, 32)
			if err == nil {
				return int32(n)
			}
		}
	}
	return def
}
