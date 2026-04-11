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
	{"test-failover", "Phase 3: kill VM agent, verify LXC takes over, restore"},
	{"remove-keepalived", "Phase 4: stop/disable keepalived on VM and LXC"},
	{"status", "Show current state of all components"},
	{"rollback", "Emergency: re-enable OPNsense gateway, restart keepalived"},
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
	case "test-failover":
		return cmdTestFailover(ctx, log, cfg)
	case "remove-keepalived":
		return cmdRemoveKeepalived(ctx, log, cfg)
	case "status":
		return cmdStatus(ctx, log, cfg)
	case "rollback":
		return cmdRollback(ctx, log, cfg)
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
		ASN:       cfg.BGP.ASN,
		RouterID:  cfg.OPNsense.BGP.RouterID,
		Neighbors: neighbors,
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
	status, err := client.GetBGPStatus(ctx)
	if err != nil {
		log.Warn("could not verify BGP status (FRR may not be running yet)", "err", err)
		log.Info("BGP configuration was applied. Verify manually:")
		fmt.Fprintf(os.Stderr, "  ssh opnsense vtysh -c 'show bgp summary'\n")
	} else {
		log.Info("BGP status after configuration", "status", status)
	}

	log.Info("=== Phase 1a complete ===")
	log.Info("next step: mwan cutover2 deploy-agents")
	return nil
}

// ---------------------------------------------------------------------------
// Stubs for remaining subcommands
// ---------------------------------------------------------------------------

func cmdDeployAgents(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 1b+1c: Deploy agents ===")
	log.Warn("not yet implemented -- will deploy mwan binary + config to VM and LXC via SSH")
	log.Info("rollback: pkill mwan on VM and LXC")
	return nil
}

func cmdVerifyCoexistence(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 1d: Verify coexistence ===")
	log.Warn("not yet implemented -- will check all BGP peers established, traffic still on static")
	log.Info("rollback: none needed (read-only check)")
	return nil
}

func cmdSwitchToBGP(ctx context.Context, log *slog.Logger, cfg *config.Config) error {
	log.Info("=== Phase 2: Switch to BGP ===")
	log.Warn("not yet implemented -- will disable OPNsense gateway via API, verify BGP takes over")
	log.Info("rollback: mwan cutover2 rollback (re-enables OPNsense gateway)")
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
	log.Info("=== Emergency rollback ===")
	log.Warn("not yet implemented -- will re-enable OPNsense gateway and restart keepalived")
	return nil
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

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
