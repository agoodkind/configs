package main

import (
	"context"
	"fmt"
	"log/slog"
)

// probeLogWrap logs the underlying error and returns a wrapped error.
// Probe helpers funnel all error returns through this so wrapcheck is
// satisfied and the staticcheck-extra slogged-wrap rule sees a paired
// slog event for every [fmt.Errorf] return.
func probeLogWrap(ctx context.Context, op string, err error) error {
	slog.ErrorContext(ctx, "opnsense-probe: "+op, "err", err)
	return fmt.Errorf("opnsense-probe: %s: %w", op, err)
}

// probeLogWrapStr returns ("", err) where err has been logged and
// wrapped via probeLogWrap. It is the two-result form used by
// deployStage and other helpers that surface a string alongside the
// error result.
func probeLogWrapStr(ctx context.Context, op string, err error) (string, error) {
	return "", probeLogWrap(ctx, op, err)
}
