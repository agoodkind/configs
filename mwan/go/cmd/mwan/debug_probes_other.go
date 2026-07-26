//go:build !linux

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/netif"
)

func runDebugProbeView(
	ctx context.Context,
	_ io.Writer,
	logger *slog.Logger,
	_ *config.Config,
	_ string,
	_ []string,
) error {
	_, err := netif.HTTPGet(ctx, "", "", "", 0)
	if err != nil {
		logger.WarnContext(
			ctx,
			"debug: active probes are unavailable",
			"err",
			err,
		)
		return fmt.Errorf("active debug probes require Linux: %w", err)
	}
	return fmt.Errorf("active debug probes require Linux")
}
