//go:build !linux

package agent

import (
	"context"
	"log/slog"

	"goodkind.io/mwan/internal/bgp"
	"goodkind.io/mwan/internal/config"
)

func configurePlatformBGP(_ context.Context, _ *config.Config, _ *bgp.Speaker, _ *slog.Logger) {}
