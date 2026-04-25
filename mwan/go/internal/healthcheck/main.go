// Package healthcheck provides continuous connectivity testing with structured logging.
//
// Designed for cutover validation: creates new connections each iteration (no long-lived
// sessions) to exercise the full routing path. Tests IPv4 ping, IPv6 ping, and HTTP
// against diverse targets, rotating each cycle.
package healthcheck

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"goodkind.io/mwan/internal/version"
)

var (
	defaultV4Targets = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9", "208.67.222.222"}
	defaultV6Targets = []string{"2606:4700:4700::1111", "2001:4860:4860::8888", "2620:fe::fe", "2620:119:35::35"}
	defaultHTTPSites = []string{"http://ifconfig.co/ip", "http://icanhazip.com", "http://ipv4.google.com", "http://ipv6.google.com"}
)

const defaultInterval = 500 * time.Millisecond

// Run starts the health check loop.
func Run() error {
	interval := defaultInterval

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger = logger.With("build", version.BuildVersionString())
	slog.SetDefault(logger)

	slog.Info("health-check started", "interval", interval.String(),
		"v4_targets", len(defaultV4Targets), "v6_targets", len(defaultV6Targets), "http_sites", len(defaultHTTPSites))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	httpClient := &http.Client{Timeout: 3 * time.Second}
	i := 0

	for {
		select {
		case <-ctx.Done():
			slog.Info("health-check stopping")
			return nil
		default:
		}

		v4Target := defaultV4Targets[i%len(defaultV4Targets)]
		v6Target := defaultV6Targets[i%len(defaultV6Targets)]
		httpSite := defaultHTTPSites[i%len(defaultHTTPSites)]

		v4ok := ping4(ctx, v4Target)
		v6ok := ping6(ctx, v6Target)
		httpCode := httpCheck(httpClient, httpSite)
		httpOK := httpCode == 200

		allOK := v4ok && v6ok && httpOK

		level := slog.LevelInfo
		if !allOK {
			level = slog.LevelError
		}

		logger.Log(ctx, level, "health",
			slog.Group("v4", "target", v4Target, "ok", v4ok),
			slog.Group("v6", "target", v6Target, "ok", v6ok),
			slog.Group("http", "site", httpSite, "code", httpCode, "ok", httpOK),
		)

		i++
		time.Sleep(interval)
	}
}

func ping4(ctx context.Context, host string) bool {
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "2", host)
	return cmd.Run() == nil
}

func ping6(ctx context.Context, host string) bool {
	cmd := exec.CommandContext(ctx, "ping6", "-c", "1", "-W", "2", host)
	if cmd.Run() == nil {
		return true
	}
	// Some systems use "ping -6" instead of "ping6"
	cmd = exec.CommandContext(ctx, "ping", "-6", "-c", "1", "-W", "2", host)
	return cmd.Run() == nil
}

func httpCheck(client *http.Client, url string) int {
	resp, err := client.Get(url) //nolint:gosec,noctx // URL from hardcoded list, short-lived check
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

