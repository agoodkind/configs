package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// CutoverConfig holds all addresses, credentials, and identifiers for the cutover.
// Loaded from a TOML file so the same binary works on suburban (testbed) and vault (production).
type CutoverConfig struct {
	Hostname string `toml:"hostname"` // "suburban" or "vault", for email context

	// VM (primary MWAN)
	MwanVMID     string `toml:"mwan_vmid"`
	MwanMgmtAddr string `toml:"mwan_mgmt_addr"` // SSH address (management network)
	MwanIntIface string `toml:"mwan_int_iface"`  // internal interface (enmwanbr0 or ens20)

	// Current production state (pre-cutover)
	CurrentRealIPv6 string `toml:"current_real_ipv6"`
	CurrentRealIPv4 string `toml:"current_real_ipv4"`

	// New addresses (post-cutover)
	NewRealIPv6 string `toml:"new_real_ipv6"`
	NewRealIPv4 string `toml:"new_real_ipv4"`
	VIPIPv6     string `toml:"vip_ipv6"`
	VIPIPv4     string `toml:"vip_ipv4"`

	// OPNsense (for NDP cache flush during rollback)
	OPNsenseAddr string `toml:"opnsense_addr"` // SSH address (e.g. agoodkind@3d06:bad:b01::1)
	OPNsenseVIPv6 string `toml:"opnsense_vip_v6"` // VIP address to flush from NDP (e.g. 3d06:bad:b01:fe::1)

	// Failover LXC
	FailoverLXCID       string `toml:"failover_lxc_id"`
	FailoverLXCIface    string `toml:"failover_lxc_iface"`     // internal iface (eth1)
	FailoverLXCWanIface string `toml:"failover_lxc_wan_iface"` // WAN iface (eth0)
	FailoverDefaultGW6  string `toml:"failover_default_gw6"`   // IPv6 default route gateway (Monkeybrains LL)
	FailoverDefaultGW4  string `toml:"failover_default_gw4"`   // IPv4 default route gateway
	FailoverInternalPfx string `toml:"failover_internal_pfx"`  // internal return route prefix (3d06:bad:b01::/60)
	FailoverOPNsenseLL  string `toml:"failover_opnsense_ll"`   // OPNsense link-local on mwanbr (next-hop for internal)
	FailoverIPv4Return  string `toml:"failover_ipv4_return"`   // IPv4 return route (e.g. 10.250.0.0/16 via 10.250.250.2)

	// keepalived
	VRID           int `toml:"vrid"`
	MasterPriority int `toml:"master_priority"`
	BackupPriority int `toml:"backup_priority"`
	AdvertInterval int `toml:"advert_interval"`

	// Verification + health check targets
	PingTargetIPv6 string   `toml:"ping_target_ipv6"`
	PingTargetIPv4 string   `toml:"ping_target_ipv4"`
	PingTargets    []string `toml:"ping_targets"` // health check uses all of these
	CurlTarget     string   `toml:"curl_target"`

	// Health check
	HealthCheckInterval int `toml:"health_check_interval"` // seconds between checks
	HealthCheckWeight   int `toml:"health_check_weight"`   // negative priority adjustment on failure
	HealthCheckFall     int `toml:"health_check_fall"`     // consecutive failures before unhealthy
	HealthCheckRise     int `toml:"health_check_rise"`     // consecutive successes before healthy

	// Email
	SMTP2GOAPIKey  string `toml:"-"` // from env only, never in config file
	AlertEmail     string `toml:"alert_email"`
	EmailFrom      string `toml:"email_from"`
	EmailBindIface string `toml:"email_bind_iface"` // interface for OOB email (e.g. "mbrains")

	// Timeouts
	SSHTimeoutSec    int `toml:"ssh_timeout_sec"`
	VerifyTimeoutSec int `toml:"verify_timeout_sec"`
	BootWaitSec      int `toml:"boot_wait_sec"`

	// Flags (from CLI, not config file)
	DryRun bool `toml:"-"`
}

func loadCutoverConfig() (*CutoverConfig, error) {
	// Find config file: --config flag, or default path
	configPath := "/etc/mwan-cutover/config.toml"
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			configPath = os.Args[i+1]
			// Remove --config and its value from os.Args
			os.Args = append(os.Args[:i], os.Args[i+2:]...)
			break
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", configPath, err)
	}

	cfg := &CutoverConfig{
		// Defaults
		VRID:             51,
		MasterPriority:   150,
		BackupPriority:   50,
		AdvertInterval:   1,
		PingTargetIPv6:      "2606:4700:4700::1111",
		PingTargetIPv4:      "1.1.1.1",
		PingTargets:         []string{"2606:4700:4700::1111", "2001:4860:4860::8888"},
		CurlTarget:          "https://ifconfig.co/ip",
		HealthCheckInterval: 10,
		HealthCheckWeight:   -110,
		HealthCheckFall:     3,
		HealthCheckRise:     2,
		AlertEmail:       emailTo,
		EmailFrom:        emailFrom,
		SSHTimeoutSec:    10,
		VerifyTimeoutSec: 30,
		BootWaitSec:      35,
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", configPath, err)
	}

	// Secrets from env only
	cfg.SMTP2GOAPIKey = strings.TrimSpace(os.Getenv("SMTP2GO_API_KEY"))
	if cfg.SMTP2GOAPIKey == "" {
		return nil, errors.New("SMTP2GO_API_KEY env var required")
	}

	// CLI flags
	for _, arg := range os.Args[1:] {
		if arg == "--dry-run" {
			cfg.DryRun = true
		}
	}

	// Validate required fields
	required := map[string]string{
		"mwan_vmid":         cfg.MwanVMID,
		"mwan_mgmt_addr":    cfg.MwanMgmtAddr,
		"mwan_int_iface":    cfg.MwanIntIface,
		"current_real_ipv6": cfg.CurrentRealIPv6,
		"vip_ipv6":          cfg.VIPIPv6,
		"failover_lxc_id":   cfg.FailoverLXCID,
	}
	for k, v := range required {
		if v == "" {
			return nil, fmt.Errorf("config field %q is required", k)
		}
	}

	return cfg, nil
}
