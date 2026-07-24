//go:build !linux

// Package netif provides portable probe entry points for non-Linux builds.
package netif

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"time"
)

// Ping4 reports that raw interface-bound probes require Linux.
func Ping4(
	_ context.Context, _ string, _ netip.Addr, _ time.Duration,
) (time.Duration, error) {
	return 0, fmt.Errorf("Ping4: raw ICMP probes require Linux")
}

// Ping6 reports that the proven V6Probe implementation requires Linux.
func Ping6(
	_ context.Context, _ string, _ netip.Addr, _ time.Duration,
) (time.Duration, error) {
	return 0, fmt.Errorf("Ping6: raw ICMP probes require Linux")
}

// HTTPCheck preserves default-route HTTP probes on non-Linux builds.
func HTTPCheck(
	ctx context.Context, iface string, url string, timeout time.Duration,
) (int, error) {
	if iface != "" {
		return 0, fmt.Errorf("HTTPCheck: interface binding requires Linux")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.WarnContext(ctx, "netif: HTTPCheck request failed", "err", err)
		return 0, fmt.Errorf("HTTPCheck request: %w", err)
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		slog.WarnContext(ctx, "netif: HTTPCheck GET failed", "err", err)
		return 0, fmt.Errorf("HTTPCheck GET %q: %w", url, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	return response.StatusCode, nil
}
