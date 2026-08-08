//go:build !linux

package agent

import (
	"log/slog"

	"goodkind.io/mwan/internal/bgp"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/notify"
)

func configurePlatformBGP(_ *config.Config, _ *bgp.Speaker, _ *slog.Logger) {}

func startPlatformBGPAudit(
	_ *config.Config,
	_ *bgp.Speaker,
	_ notify.Notifier,
	_ *slog.Logger,
) func() {
	return nil
}
