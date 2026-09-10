//go:build !linux

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"goodkind.io/mwan/internal/config"
)

func runDebugProbeView(
	ctx context.Context,
	_ io.Writer,
	logger *slog.Logger,
	_ *config.Config,
	_ string,
	_ []string,
) error {
	return fmt.Errorf("active debug probes require Linux")
}
