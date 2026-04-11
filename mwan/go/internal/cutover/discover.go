package cutover

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"goodkind.io/mwan/internal/config"
)

// discoverRuntime populates dynamic fields in the config.Cutover by SSHing
// into the relevant hosts and querying their actual state. Only fields that
// can change between reboots (link-locals, current addresses) are discovered.
// Static topology (VMIDs, interface names, VRIDs) stays in the TOML.
func discoverRuntime(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	to := cfg.Cutover.SSHTimeoutSec

	if err := discoverVMAddresses(ctx, log, cfg, to); err != nil {
		return err
	}
	discoverOPNsenseLL(ctx, log, cfg, to)
	discoverFailoverGW6(ctx, log, cfg, to)
	discoverFailoverGW4(ctx, log, cfg, to)

	return nil
}

// discoverVMAddresses queries the primary VM's internal interface for current IPv6 and IPv4 addresses.
func discoverVMAddresses(ctx context.Context, log *slog.Logger, cfg *config.Config, to int) error {
	log.Info("discover: querying VM addresses", "host", cfg.MwanMgmtAddr, "iface", cfg.MwanIntIface)
	addrOut, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
		fmt.Sprintf("ip addr show %s", cfg.MwanIntIface), to)
	if err != nil {
		return fmt.Errorf("discover VM addresses: %w", err)
	}

	if cfg.Cutover.CurrentRealIPv6 == "" {
		v6 := parseFirstGUA(addrOut)
		if v6 != "" {
			cfg.Cutover.CurrentRealIPv6 = v6
			log.Info("discover: current_real_ipv6", "addr", v6)
		}
	}

	if cfg.Cutover.CurrentRealIPv4 == "" {
		v4 := parseFirstV4(addrOut)
		if v4 != "" {
			cfg.Cutover.CurrentRealIPv4 = v4
			log.Info("discover: current_real_ipv4", "addr", v4)
		}
	}

	return nil
}

// discoverOPNsenseLL queries OPNsense for its WAN link-local address (used for NDP flush).
func discoverOPNsenseLL(ctx context.Context, log *slog.Logger, cfg *config.Config, to int) {
	if cfg.Cutover.OPNsenseAddr == "" || cfg.Cutover.FailoverOPNsenseLL != "" {
		return
	}
	log.Info("discover: querying OPNsense link-local")
	opnOut, opnErr := sshExec(ctx, cfg.Cutover.OPNsenseAddr,
		"ifconfig | grep -A1 'description: WAN' | grep fe80 | awk '{print $2}' | cut -d% -f1", to)
	if opnErr == nil && opnOut.Stdout != "" {
		cfg.Cutover.FailoverOPNsenseLL = opnOut.Stdout
		log.Info("discover: failover_opnsense_ll", "ll", opnOut.Stdout)
	}
}

// discoverFailoverGW6 queries the failover LXC for its IPv6 default gateway link-local.
func discoverFailoverGW6(ctx context.Context, log *slog.Logger, cfg *config.Config, to int) {
	if cfg.Cutover.FailoverLXCID == "" || cfg.Cutover.FailoverDefaultGW6 != "" {
		return
	}
	log.Info("discover: querying failover LXC WAN gateway")
	gwOut, gwErr := localExec(ctx, "pct", []string{"exec", cfg.Cutover.FailoverLXCID, "--",
		"ip", "-6", "route", "show", "default"}, to)
	if gwErr == nil && gwOut != "" {
		gw := parseViaLL(gwOut)
		if gw != "" {
			cfg.Cutover.FailoverDefaultGW6 = gw
			log.Info("discover: failover_default_gw6", "gw", gw)
		}
	}
}

// discoverFailoverGW4 queries the failover LXC for its IPv4 default gateway.
func discoverFailoverGW4(ctx context.Context, log *slog.Logger, cfg *config.Config, to int) {
	if cfg.Cutover.FailoverLXCID == "" || cfg.Cutover.FailoverDefaultGW4 != "" {
		return
	}
	gwOut, gwErr := localExec(ctx, "pct", []string{"exec", cfg.Cutover.FailoverLXCID, "--",
		"ip", "-4", "route", "show", "default"}, to)
	if gwErr == nil && gwOut != "" {
		gw := parseViaV4(gwOut)
		if gw != "" {
			cfg.Cutover.FailoverDefaultGW4 = gw
			log.Info("discover: failover_default_gw4", "gw", gw)
		}
	}
}

// parseFirstGUA extracts the first global unicast IPv6 address (with prefix) from ip addr output.
// Returns e.g. "3d06:bad:b01:201::3/64" or "".
func parseFirstGUA(ipAddrOutput string) string {
	for _, line := range strings.Split(ipAddrOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet6 ") && strings.Contains(line, "scope global") && !strings.Contains(line, "dadfailed") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1] // e.g. "3d06:bad:b01:201::3/64"
			}
		}
	}
	return ""
}

// parseFirstV4 extracts the first IPv4 address (with prefix) from ip addr output.
func parseFirstV4(ipAddrOutput string) string {
	for _, line := range strings.Split(ipAddrOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") && !strings.Contains(line, "127.0.0.1") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1] // e.g. "10.250.250.3/29"
			}
		}
	}
	return ""
}

// parseViaLL extracts a link-local next-hop from "default via fe80::xxx dev ..." output.
func parseViaLL(routeOutput string) string {
	for _, line := range strings.Split(routeOutput, "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "via" && i+1 < len(fields) && strings.HasPrefix(fields[i+1], "fe80::") {
				return fields[i+1]
			}
		}
	}
	return ""
}

// parseViaV4 extracts an IPv4 next-hop from "default via x.x.x.x dev ..." output.
func parseViaV4(routeOutput string) string {
	for _, line := range strings.Split(routeOutput, "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "via" && i+1 < len(fields) && !strings.Contains(fields[i+1], ":") {
				return fields[i+1]
			}
		}
	}
	return ""
}
