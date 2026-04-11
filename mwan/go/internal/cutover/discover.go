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

	// 1. Discover current addresses on the primary VM's internal interface
	log.Info("discover: querying VM addresses", "host", cfg.MwanMgmtAddr, "iface", cfg.MwanIntIface)
	addrOut, err := sshMustExec(ctx, cfg.MwanMgmtAddr,
		fmt.Sprintf("ip addr show %s", cfg.MwanIntIface), to)
	if err != nil {
		return fmt.Errorf("discover VM addresses: %w", err)
	}

	// Parse current IPv6 GUA on the internal interface (the "real" address)
	if cfg.Cutover.CurrentRealIPv6 == "" {
		v6 := parseFirstGUA(addrOut)
		if v6 != "" {
			cfg.Cutover.CurrentRealIPv6 = v6
			log.Info("discover: current_real_ipv6", "addr", v6)
		}
	}

	// Parse current IPv4 on the internal interface
	if cfg.Cutover.CurrentRealIPv4 == "" {
		v4 := parseFirstV4(addrOut)
		if v4 != "" {
			cfg.Cutover.CurrentRealIPv4 = v4
			log.Info("discover: current_real_ipv4", "addr", v4)
		}
	}

	// 2. Discover OPNsense link-local on the mwanbr segment (for NDP flush)
	if cfg.Cutover.OPNsenseAddr != "" && cfg.Cutover.FailoverOPNsenseLL == "" {
		log.Info("discover: querying OPNsense link-local")
		// Get OPNsense's WAN interface link-local via SSH
		opnOut, opnErr := sshExec(ctx, cfg.Cutover.OPNsenseAddr,
			"ifconfig | grep -A1 'description: WAN' | grep fe80 | awk '{print $2}' | cut -d% -f1", to)
		if opnErr == nil && opnOut.Stdout != "" {
			cfg.Cutover.FailoverOPNsenseLL = opnOut.Stdout
			log.Info("discover: failover_opnsense_ll", "ll", opnOut.Stdout)
		}
	}

	// 3. Discover failover LXC WAN gateway link-local
	if cfg.Cutover.FailoverLXCID != "" && cfg.Cutover.FailoverDefaultGW6 == "" {
		log.Info("discover: querying failover LXC WAN gateway")
		wanIface := cfg.Cutover.FailoverLXCWanIface
		if wanIface == "" {
			wanIface = "eth0"
		}
		// The LXC's default gateway LL is the ISP LXC on the same bridge
		gwOut, gwErr := localExec(ctx, "pct", []string{"exec", cfg.Cutover.FailoverLXCID, "--",
			"ip", "-6", "route", "show", "default"}, to)
		if gwErr == nil && gwOut != "" {
			// Parse "default via fe80::xxx dev eth0 ..."
			gw := parseViaLL(gwOut)
			if gw != "" {
				cfg.Cutover.FailoverDefaultGW6 = gw
				log.Info("discover: failover_default_gw6", "gw", gw)
			}
		}
	}

	// 4. Discover failover LXC IPv4 gateway
	if cfg.Cutover.FailoverLXCID != "" && cfg.Cutover.FailoverDefaultGW4 == "" {
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

	return nil
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
