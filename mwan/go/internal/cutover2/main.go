// Package cutover2 orchestrates the VRRP-to-BGP migration.
//
// Unlike the original cutover package (keepalived-based HA), cutover2
// migrates from VRRP/keepalived to embedded GoBGP with OPNsense FRR.
// Each subcommand maps to a phase in the migration plan and is designed
// to be run independently, verified, and rolled back.
package cutover2

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/logging"
	"goodkind.io/mwan/internal/opnsense"
	"goodkind.io/mwan/internal/ops"
	"goodkind.io/mwan/internal/tracing"
	"goodkind.io/mwan/internal/version"
)

const globalTimeout = 5 * time.Minute

var subcommands = []struct {
	name string
	desc string
}{
	{"configure-opnsense", "Phase 1a: configure FRR/BGP on OPNsense via API"},
	{"deploy-agents", "Phase 1b+1c: deploy mwan binary + config to VM and LXC"},
	{"verify-coexistence", "Phase 1d: check all BGP peers established, traffic still on static"},
	{"switch-to-bgp", "Phase 2: disable OPNsense gateway, verify BGP takes over"},
	{"arm-watchdog", "Post-cutover: probe connectivity, snapshot VM, start watchdog"},
	{"disarm-watchdog", "Stop mwan-watchdog (use before maintenance, after issues)"},
	{"test-failover", "Phase 3: kill VM agent, verify LXC takes over, restore"},
	{"remove-keepalived", "Phase 4: stop/disable keepalived on VM and LXC"},
	{"status", "Show current state of all components"},
	{"rollback", "Emergency: re-enable OPNsense gateway, restart keepalived"},
	{"unfuck", "Nuclear rollback: stop agents, stop FRR, re-enable gateway, verify"},
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mwan cutover2 <subcommand>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Subcommands:")
	for _, sc := range subcommands {
		fmt.Fprintf(os.Stderr, "  %-24s %s\n", sc.name, sc.desc)
	}
}

// Run is the entry point called from cmd/mwan/main.go.
func Run(cfg *config.Config) error {
	log, err := logging.New(logging.Config{
		JSONLogFile: "/var/log/mwan-cutover2.log",
	}, version.BuildVersionString())
	if err != nil {
		return fmt.Errorf("logger init: %w", err)
	}
	runID := tracing.NewID()
	log = log.With(
		slog.String(tracing.RunIDKey, runID),
		slog.String(tracing.ComponentKey, "cutover2"),
	)

	if len(os.Args) < 2 {
		usage()
		return fmt.Errorf("missing subcommand")
	}
	sub := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	ctx, cancelTO := context.WithTimeout(ctx, globalTimeout)
	defer cancelTO()

	log.Info("cutover2", "subcommand", sub, "build", version.BuildVersion())

	switch sub {
	case "configure-opnsense":
		return cmdConfigureOPNsense(ctx, log, cfg)
	case "deploy-agents":
		return cmdDeployAgents(ctx, log, cfg)
	case "verify-coexistence":
		return cmdVerifyCoexistence(ctx, log, cfg)
	case "switch-to-bgp":
		return cmdSwitchToBGP(ctx, log, cfg)
	case "arm-watchdog":
		return cmdArmWatchdog(ctx, log, cfg)
	case "disarm-watchdog":
		return cmdDisarmWatchdog(ctx, log, cfg)
	case "test-failover":
		return cmdTestFailover(ctx, log, cfg)
	case "remove-keepalived":
		return cmdRemoveKeepalived(ctx, log, cfg)
	case "status":
		return cmdStatus(ctx, log, cfg)
	case "rollback":
		return cmdRollback(ctx, log, cfg)
	case "unfuck":
		return cmdUnfuck(ctx, log, cfg)
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}

// ---------------------------------------------------------------------------
// configure-opnsense: Phase 1a
// ---------------------------------------------------------------------------

func cmdConfigureOPNsense(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 1a: Configure FRR/BGP on OPNsense ===")

	// Validate required config.
	if err := validateOPNsenseConfig(cfg); err != nil {
		return err
	}
	if err := validateBGPConfig(cfg); err != nil {
		return err
	}

	// Build OPNsense client.
	client := opnsense.New(opnsense.Config{
		URL:       cfg.OPNsense.URL,
		APIKey:    cfg.OPNsense.APIKey,
		APISecret: cfg.OPNsense.APISecret,
		Insecure:  cfg.OPNsense.Insecure,
	}, log)

	// Build OPNsense BGP config from [opnsense.bgp] section (not [bgp]).
	// [bgp] is the agent's speaker config. [opnsense.bgp] is OPNsense's perspective.
	log.Info("will configure BGP on OPNsense",
		"url", cfg.OPNsense.URL,
		"asn", cfg.BGP.ASN,
		"router_id", cfg.OPNsense.BGP.RouterID,
		"neighbor_count", len(cfg.OPNsense.BGP.Neighbors),
	)

	var neighbors []opnsense.BGPNeighborConfig
	for _, n := range cfg.OPNsense.BGP.Neighbors {
		routeMap := "PREFER-BACKUP"
		if n.Preference == "primary" {
			routeMap = "PREFER-PRIMARY"
		}
		neighbors = append(neighbors, opnsense.BGPNeighborConfig{
			Address:     n.Address,
			RemoteAS:    cfg.BGP.ASN,
			Keepalive:   int(cfg.BGP.KeepaliveSeconds),
			Holddown:    int(cfg.BGP.HoldSeconds),
			BFD:         true,
			RouteMapIn:  routeMap,
			Description: n.Description,
		})
	}

	bgpCfg := opnsense.BGPConfig{
		ASN:              cfg.BGP.ASN,
		RouterID:         cfg.OPNsense.BGP.RouterID,
		Neighbors:        neighbors,
		FirewallSourceV4: cfg.OPNsense.BGP.FirewallSourceV4,
		FirewallSourceV6: cfg.OPNsense.BGP.FirewallSourceV6,
	}

	// Execute.
	log.Info("configuring BGP via OPNsense API...")
	if err := client.ConfigureBGP(ctx, bgpCfg); err != nil {
		log.Error("FAILED to configure BGP on OPNsense", "err", err)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "ROLLBACK: Disable BGP via OPNsense API:")
		fmt.Fprintf(os.Stderr, "  curl -k -u $KEY:$SECRET -X POST %s/api/quagga/bgp/set -d '{\"bgp\":{\"enabled\":\"0\"}}'\n", cfg.OPNsense.URL)
		fmt.Fprintf(os.Stderr, "  curl -k -u $KEY:$SECRET -X POST %s/api/quagga/service/reconfigure\n", cfg.OPNsense.URL)
		return fmt.Errorf("configure-opnsense: %w", err)
	}

	// Verify by querying BGP status.
	log.Info("verifying BGP configuration...")
	summary, err := client.GetBGPStatus(ctx)
	if err != nil {
		log.Warn("could not verify BGP status (FRR may not be running yet)", "err", err)
		log.Info("BGP configuration was applied. Verify manually:")
		fmt.Fprintf(os.Stderr, "  ssh opnsense vtysh -c 'show bgp summary'\n")
	} else {
		logBGPSummary(log, summary)
	}

	log.Info("=== Phase 1a complete ===")
	log.Info("next step: mwan cutover2 deploy-agents")
	return nil
}

// ---------------------------------------------------------------------------
// Stubs for remaining subcommands
// ---------------------------------------------------------------------------

func cmdDeployAgents(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 1b+1c: Verify agents are running with BGP ===")
	log.Info("NOTE: binary + config deployment is handled by deploy.sh (SCP).")
	log.Info("This command verifies agents are reachable via gRPC and BGP is active.")

	sysOps := buildOps(cfg, log)

	// Phase 1b: Check VM agent via gRPC
	log.Info("--- Phase 1b: Verify VM agent ---", "vmid", cfg.MwanVMID)

	vmBGP, err := sysOps.GetBGPStatus(ctx, cfg.MwanVMID)
	if err != nil {
		return fmt.Errorf("VM agent unreachable via gRPC (deploy binary + config first): %w", err)
	}
	log.Info("VM agent BGP status",
		"announcing", vmBGP.GetAnnouncing(),
		"all_established", vmBGP.GetAllEstablished(),
		"peers", len(vmBGP.GetPeers()),
	)
	if !vmBGP.GetAllEstablished() {
		log.Warn("VM BGP peers not yet established (may need time or firewall fix)")
	}

	// Phase 1c: Check LXC agent via gRPC
	if cfg.Cutover.FailoverLXCID != "" {
		log.Info("--- Phase 1c: Verify LXC agent ---", "lxc_id", cfg.Cutover.FailoverLXCID)

		lxcBGP, err := sysOps.GetBGPStatus(ctx, cfg.Cutover.FailoverLXCID)
		if err != nil {
			log.Warn("LXC agent unreachable via gRPC (deploy binary + config first, or LXC not running)", "err", err)
		} else {
			log.Info("LXC agent BGP status",
				"announcing", lxcBGP.GetAnnouncing(),
				"all_established", lxcBGP.GetAllEstablished(),
				"peers", len(lxcBGP.GetPeers()),
			)
		}
	}

	log.Info("=== Phase 1b+1c complete ===")
	log.Info("next step: mwan cutover2 verify-coexistence")
	return nil
}

// buildOps creates a SysOps instance using the existing multi-channel pattern
// (vsock -> TCP -> PVE REST). This is the same ops layer the watchdog uses.
func buildOps(cfg *config.Config, log *slog.Logger) ops.SysOps {
	return ops.NewRealOps(cfg, nil, log)
}

func cmdVerifyCoexistence(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 1d: Verify coexistence ===")

	sysOps := buildOps(cfg, log)
	client := opnsense.New(opnsense.Config{
		URL:       cfg.OPNsense.URL,
		APIKey:    cfg.OPNsense.APIKey,
		APISecret: cfg.OPNsense.APISecret,
		Insecure:  cfg.OPNsense.Insecure,
	}, log)

	var failures []string

	// Check 1: VM agent BGP established and announcing
	log.Info("check 1: VM agent BGP status")
	vmBGP, err := sysOps.GetBGPStatus(ctx, cfg.MwanVMID)
	if err != nil {
		failures = append(failures, fmt.Sprintf("VM agent unreachable: %v", err))
	} else {
		if !vmBGP.GetAllEstablished() {
			failures = append(failures, "VM BGP peers not all established")
		}
		if !vmBGP.GetAnnouncing() {
			failures = append(failures, "VM not announcing routes")
		}
		log.Info("VM BGP", "established", vmBGP.GetAllEstablished(), "announcing", vmBGP.GetAnnouncing())
	}

	// Check 2: OPNsense has BGP routes
	log.Info("check 2: OPNsense BGP routes")
	bgpSummary, err := client.GetBGPStatus(ctx)
	if err != nil {
		log.Warn("could not query OPNsense BGP status via API", "err", err)
	} else {
		logBGPSummary(log, bgpSummary)
	}

	// Check 3: LXC agent (if configured)
	if cfg.Cutover.FailoverLXCID != "" {
		log.Info("check 3: LXC agent BGP status")
		lxcBGP, lxcErr := sysOps.GetBGPStatus(ctx, cfg.Cutover.FailoverLXCID)
		if lxcErr != nil {
			log.Warn("LXC agent unreachable (may not be deployed yet)", "err", lxcErr)
		} else {
			log.Info("LXC BGP", "established", lxcBGP.GetAllEstablished(), "announcing", lxcBGP.GetAnnouncing())
		}
	}

	if len(failures) > 0 {
		for _, f := range failures {
			log.Error("FAILED", "check", f, "err", "coexistence check failed")
		}
		return fmt.Errorf("coexistence verification failed: %d checks failed", len(failures))
	}

	log.Info("=== Phase 1d: All checks passed ===")
	log.Info("BGP routes coexist with static route. Traffic is unaffected.")
	log.Info("next step: mwan cutover2 switch-to-bgp")
	return nil
}

func cmdSwitchToBGP(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 2: Switch to BGP ===")

	if err := validateOPNsenseConfig(cfg); err != nil {
		return err
	}
	if len(cfg.OPNsense.GatewayNames) == 0 {
		return fmt.Errorf("[opnsense] gateway_name is required for switch-to-bgp")
	}

	// Stop watchdog: it could trigger VM snapshot rollback during the cutover gap.
	log.Info("stopping mwan-watchdog on hypervisor...",
		"service", cfg.Watchdog.ServiceName)
	stopWatchdog(log, cfg.Watchdog.ServiceName)

	// Notify watchdog timestamp in case it restarts.
	writeDeployTimestamp(log, cfg)

	client := opnsense.New(opnsense.Config{
		URL:       cfg.OPNsense.URL,
		APIKey:    cfg.OPNsense.APIKey,
		APISecret: cfg.OPNsense.APISecret,
		Insecure:  cfg.OPNsense.Insecure,
	}, log)

	// Start auto-rollback monitor. If connectivity drops for >45s, unfuck triggers.
	// Paused during the reboot window to avoid false triggers.
	rollbackCtx, rollbackCancel := context.WithCancel(ctx)
	defer rollbackCancel()
	monitor := startHealthMonitor(rollbackCtx, log, func() {
		log.Error("AUTO-ROLLBACK: triggering unfuck due to prolonged connectivity loss",
			"err", "auto-rollback threshold exceeded")
		_ = cmdUnfuck(context.Background(), log, cfg)
	}, rollbackCancel)
	defer monitor.Stop()

	// Pre-check: verify BGP routes exist before touching the gateway.
	log.Info("pre-check: verifying BGP routes exist on OPNsense...")
	summary, err := client.GetBGPStatus(ctx)
	if err != nil {
		return fmt.Errorf("cannot verify BGP routes before cutover: %w", err)
	}

	v4ok := summary.IPv4Unicast != nil && summary.IPv4Unicast.RIBCount > 0
	v6ok := summary.IPv6Unicast != nil && summary.IPv6Unicast.RIBCount > 0
	if !v4ok {
		return fmt.Errorf("no IPv4 BGP routes on OPNsense, aborting cutover")
	}
	log.Info("BGP routes confirmed",
		"ipv4_routes", summary.IPv4Unicast.RIBCount,
		"ipv6_routes_present", v6ok,
	)

	// Step 1: Mark gateways as "force down" via API.
	// Persists in config.xml, survives reboot. Prevents static route
	// reinstallation when OPNsense comes back up.
	for _, gwName := range cfg.OPNsense.GatewayNames {
		log.Debug("finding gateway...", "name", gwName)
		gwUUID, _, findErr := client.FindGatewayByNameWithAddr(ctx, gwName)
		if findErr != nil {
			return fmt.Errorf("find gateway %q: %w", gwName, findErr)
		}
		log.Debug("marking gateway as force_down...", "name", gwName, "uuid", gwUUID)
		if err := client.ForceDownGateway(ctx, gwUUID); err != nil {
			return fmt.Errorf("force_down gateway %q: %w", gwName, err)
		}
	}

	// Step 2: Remove gatewayv6 from OPNsense config.xml.
	// Prevents IPv6 static route from being recreated on boot. force_down
	// only covers IPv4 gateways; the IPv6 static route comes from the
	// <gatewayv6> element in the WAN interface config.
	removeGatewayV6(ctx, log, cfg)

	// Step 3: Reboot OPNsense.
	// A clean reboot avoids the FreeBSD zebra stale route cache issue entirely.
	// On boot: force_down gateways produce no static IPv4 route, gatewayv6
	// removal produces no static IPv6 route, FRR starts fresh with an empty
	// zebra RIB, BGP peers establish, and BGP routes install without competition.
	monitor.Pause()
	log.Info("rebooting OPNsense (clean zebra start, no stale routes)...")
	if err := opnsenseSSH(ctx, log, cfg, "reboot"); err != nil {
		// reboot often kills the SSH connection before the reply arrives.
		// That produces an exit error, which is expected.
		log.Info("reboot command sent (SSH disconnect is expected)", "err", err)
	}

	// Step 4: Wait for OPNsense to come back up.
	log.Info("waiting for OPNsense to reboot (polling API)...")
	// Initial delay: OPNsense needs time to shut down before we start polling.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
	}

	if err := client.WaitForReady(ctx, 4*time.Minute, 10*time.Second); err != nil {
		monitor.Resume()
		return fmt.Errorf("OPNsense did not come back after reboot: %w", err)
	}

	// Step 5: Wait for FRR to start and BGP peers to establish.
	// FRR auto-starts on boot. BGP peers need ~30s to establish after FRR is up.
	log.Info("OPNsense is up, waiting for BGP to establish (30s)...")
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
	}

	monitor.Resume()

	// Verify BGP route is now the active default.
	log.Info("verifying BGP took over...")
	postSummary, err := client.GetBGPStatus(ctx)
	if err != nil {
		log.Warn("could not verify post-cutover BGP status", "err", err)
	} else {
		logBGPSummary(log, postSummary)
	}

	log.Info("=== Phase 2 complete: BGP is now the active routing source ===")
	log.Info("watchdog is currently STOPPED (auto-rollback OFF) " +
		"so it can't undo this cutover before you've verified it.")
	log.Info("rollback: mwan cutover2 rollback (or qm rollback 101 <pre-cutover-snapshot>)")
	log.Info("next step: VERIFY traffic, then run: mwan cutover2 arm-watchdog")
	log.Info("optional: mwan cutover2 test-failover")
	return nil
}

func cmdTestFailover(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 3: Test failover ===")
	log.Warn("not yet implemented -- will kill VM agent, verify LXC takes over, restore")
	log.Info("rollback: restart mwan agent on VM, or mwan cutover2 rollback")
	return nil
}

func cmdRemoveKeepalived(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 4: Remove keepalived ===")
	log.Warn("not yet implemented -- will stop/disable keepalived on VM and LXC")
	log.Info("rollback: systemctl start keepalived on VM")
	return nil
}

func cmdStatus(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== cutover2 status ===")
	log.Warn("not yet implemented -- will show current state of all components")
	return nil
}

func cmdRollback(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Emergency rollback: re-enable OPNsense gateway ===")

	if err := validateOPNsenseConfig(cfg); err != nil {
		return err
	}
	if len(cfg.OPNsense.GatewayNames) == 0 {
		return fmt.Errorf("[opnsense] gateway_name is required for rollback")
	}

	client := opnsense.New(opnsense.Config{
		URL:       cfg.OPNsense.URL,
		APIKey:    cfg.OPNsense.APIKey,
		APISecret: cfg.OPNsense.APISecret,
		Insecure:  cfg.OPNsense.Insecure,
	}, log)

	for _, gwName := range cfg.OPNsense.GatewayNames {
		log.Debug("finding gateway...", "name", gwName)
		gwUUID, findErr := client.FindGatewayByName(ctx, gwName)
		if findErr != nil {
			return fmt.Errorf("find gateway %q: %w", gwName, findErr)
		}
		log.Debug("removing force_down from gateway...", "name", gwName, "uuid", gwUUID)
		if err := client.UnforceDownGateway(ctx, gwUUID); err != nil {
			return fmt.Errorf("unforce_down gateway %q: %w", gwName, err)
		}
	}

	if err := client.Reconfigure(ctx); err != nil {
		return fmt.Errorf("reconfigure after gateway enable: %w", err)
	}

	log.Info("=== Rollback complete: static gateway re-enabled ===")
	return nil
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

func logBGPSummary(log *slog.Logger, s *opnsense.BGPSummary) {
	for _, af := range []struct {
		name string
		data *opnsense.BGPAFSummary
	}{
		{"ipv4", s.IPv4Unicast},
		{"ipv6", s.IPv6Unicast},
	} {
		if af.data == nil {
			continue
		}
		for addr, peer := range af.data.Peers {
			log.Debug("OPNsense BGP peer",
				"af", af.name,
				"neighbor", addr,
				"state", peer.State,
				"pfx_rcvd", peer.PfxRcd,
				"uptime", peer.PeerUptime,
			)
		}
	}
}

func validateOPNsenseConfig(cfg *config.Config) error {
	if cfg.OPNsense.URL == "" {
		return fmt.Errorf("[opnsense] url is required")
	}
	if cfg.OPNsense.APIKey == "" {
		return fmt.Errorf("[opnsense] api_key is required")
	}
	if cfg.OPNsense.APISecret == "" {
		return fmt.Errorf("[opnsense] api_secret is required (set in TOML or OPNSENSE_API_SECRET env)")
	}
	return nil
}

func validateBGPConfig(cfg *config.Config) error {
	if cfg.BGP.ASN == 0 {
		return fmt.Errorf("[bgp] asn is required for cutover2")
	}
	if cfg.OPNsense.BGP.RouterID == "" {
		return fmt.Errorf("[opnsense.bgp] router_id is required")
	}
	if len(cfg.OPNsense.BGP.Neighbors) == 0 {
		return fmt.Errorf("[opnsense.bgp] at least one neighbor is required")
	}
	for _, n := range cfg.OPNsense.BGP.Neighbors {
		if n.Address == "" {
			return fmt.Errorf("[opnsense.bgp.neighbors] address is required")
		}
		if n.Preference != "primary" && n.Preference != "backup" {
			return fmt.Errorf("[opnsense.bgp.neighbors] preference must be 'primary' or 'backup', got %q", n.Preference)
		}
	}
	return nil
}
