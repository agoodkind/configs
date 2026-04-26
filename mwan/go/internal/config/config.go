package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// WANInterface describes one WAN uplink inside the MWAN VM.
type WANInterface struct {
	Name string `toml:"name"`
}

// NetworkConfig holds site-specific topology values.
type NetworkConfig struct {
	PingTargetIPv4 string         `toml:"ping_target_ipv4"`
	PingTargetIPv6 string         `toml:"ping_target_ipv6"`
	PingTargets    []string       `toml:"ping_targets"`
	CurlTarget     string         `toml:"curl_target"`
	WANInterfaces  []WANInterface `toml:"wan_interfaces"`
	LastDeployPath string         `toml:"last_deploy_path"`
	LastChangePath string         `toml:"last_change_path"`
}

func (nc NetworkConfig) WanIfaceNames() []string {
	names := make([]string, len(nc.WANInterfaces))
	for i, w := range nc.WANInterfaces {
		names[i] = w.Name
	}
	return names
}

// EmailConfig holds email notification settings.
type EmailConfig struct {
	SMTP2GOAPIKey string `toml:"smtp2go_api_key"`
	AlertEmail    string `toml:"alert_email"`
	From          string `toml:"from"`
	SubjectPrefix string `toml:"subject_prefix"`
	BindIface     string `toml:"bind_iface"`
	MinLevel      string `toml:"min_level"`
	Cooldown      string `toml:"cooldown"`
}

// PVEConfig holds Proxmox VE API credentials and endpoints.
type PVEConfig struct {
	BaseURL     string `toml:"base_url"`
	Node        string `toml:"node"`
	TokenID     string `toml:"token_id"`
	TokenSecret string `toml:"token_secret"`
}

// WatchdogSection holds watchdog-specific configuration.
type WatchdogSection struct {
	DeployWindowMinutes        int `toml:"deploy_window_minutes"`
	ConnectivityTimeoutSeconds int `toml:"connectivity_timeout_seconds"`
	CheckIntervalHealthy       int `toml:"check_interval_healthy_seconds"`
	CheckIntervalDegraded      int `toml:"check_interval_degraded_seconds"`
	PostRollbackGraceSeconds   int `toml:"post_rollback_grace_seconds"`
	AlertCooldownSeconds       int `toml:"alert_cooldown_seconds"`
	DeployGracePeriodSeconds   int `toml:"deploy_grace_period_seconds"`
	MaxRollbackAttempts        int `toml:"max_rollback_attempts"`
	MaxIterations              int `toml:"max_iterations"`

	SnapshotHealthyThreshold   int `toml:"snapshot_healthy_threshold"`
	MaxKnownGoodSnapshots      int `toml:"max_known_good_snapshots"`
	HashCheckEveryNHealthy     int `toml:"hash_check_every_n_healthy"`
	MinSnapshotIntervalSeconds int `toml:"min_snapshot_interval_seconds"`
	MaxTotalSnapshots          int `toml:"max_total_snapshots"`

	VsockCID         uint32 `toml:"vsock_cid"`
	VsockPort        uint32 `toml:"vsock_port"`
	MwanAgentTCPAddr string `toml:"mwan_agent_tcp_addr"`

	LogFile           string `toml:"log_file"`
	JSONLogFile       string `toml:"json_log_file"`
	RollbackStateFile string `toml:"rollback_state_file"`
	RollbackLockFile  string `toml:"rollback_lock_file"`

	// ServiceName is the systemd unit name that the cutover2 arm-watchdog
	// and disarm-watchdog subcommands target. Defaults to "mwan-watchdog"
	// on production. Testbed sets this to "mwan-watchdog-testbed" so the
	// same binary works against the per-environment unit.
	ServiceName string `toml:"service_name"`
}

// Duration helper methods for WatchdogSection.

func (ws WatchdogSection) HealthyInterval() time.Duration {
	return time.Duration(ws.CheckIntervalHealthy) * time.Second
}

func (ws WatchdogSection) DegradedInterval() time.Duration {
	return time.Duration(ws.CheckIntervalDegraded) * time.Second
}

func (ws WatchdogSection) PostRollbackGrace() time.Duration {
	return time.Duration(ws.PostRollbackGraceSeconds) * time.Second
}

// CutoverSection holds cutover-specific configuration.
type CutoverSection struct {
	CurrentRealIPv6 string `toml:"current_real_ipv6"`
	CurrentRealIPv4 string `toml:"current_real_ipv4"`
	NewRealIPv6     string `toml:"new_real_ipv6"`
	NewRealIPv4     string `toml:"new_real_ipv4"`
	VIPIPv6         string `toml:"vip_ipv6"`
	VIPIPv4         string `toml:"vip_ipv4"`

	OPNsenseAddr string `toml:"opnsense_addr"`

	FailoverLXCID       string `toml:"failover_lxc_id"`
	FailoverLXCIface    string `toml:"failover_lxc_iface"`
	FailoverLXCWanIface string `toml:"failover_lxc_wan_iface"`
	FailoverDefaultGW6  string `toml:"failover_default_gw6"`
	FailoverDefaultGW4  string `toml:"failover_default_gw4"`
	FailoverInternalPfx string `toml:"failover_internal_pfx"`
	FailoverOPNsenseLL  string `toml:"failover_opnsense_ll"`
	FailoverIPv4Return  string `toml:"failover_ipv4_return"`

	VRID           int `toml:"vrid"`
	MasterPriority int `toml:"master_priority"`
	BackupPriority int `toml:"backup_priority"`
	AdvertInterval int `toml:"advert_interval"`

	HealthCheckInterval int `toml:"health_check_interval"`
	HealthCheckWeight   int `toml:"health_check_weight"`
	HealthCheckFall     int `toml:"health_check_fall"`
	HealthCheckRise     int `toml:"health_check_rise"`

	SSHTimeoutSec    int `toml:"ssh_timeout_sec"`
	VerifyTimeoutSec int `toml:"verify_timeout_sec"`
	BootWaitSec      int `toml:"boot_wait_sec"`
}

// OPNsenseSection holds OPNsense API credentials, endpoint, and its own BGP config.
// OPNsense is a BGP peer, not a speaker we control. Its BGP config is the inverse
// of the agent's: different router-id, different neighbor list.
type OPNsenseSection struct {
	URL          string      `toml:"url"`
	APIKey       string      `toml:"api_key"`
	APISecret    string      `toml:"api_secret"`
	Insecure     bool        `toml:"insecure"`
	GatewayNames []string    `toml:"gateway_names"`
	BGP          OPNsenseBGP `toml:"bgp"`
}

// OPNsenseBGP describes the BGP configuration to push to OPNsense via its API.
type OPNsenseBGP struct {
	RouterID         string                `toml:"router_id"`
	Neighbors        []OPNsenseBGPNeighbor `toml:"neighbors"`
	FirewallSourceV4 string                `toml:"firewall_source_v4"` // e.g. "10.250.250.0/29"
	FirewallSourceV6 string                `toml:"firewall_source_v6"` // e.g. "3d06:bad:b01:201::/64"
}

// OPNsenseBGPNeighbor is a BGP peer from OPNsense's perspective.
type OPNsenseBGPNeighbor struct {
	Address     string `toml:"address"`
	Description string `toml:"description"`
	Preference  string `toml:"preference"` // "primary" or "backup"
}

// BGPSection holds embedded GoBGP speaker configuration.
type BGPSection struct {
	Enabled          bool          `toml:"enabled"`
	ASN              uint32        `toml:"asn"`
	RouterID         string        `toml:"router_id"`
	NextHopV6        string        `toml:"next_hop_v6"` // IPv6 next-hop for announced routes (optional, defaults to RouterID)
	KeepaliveSeconds uint32        `toml:"keepalive_seconds"`
	HoldSeconds      uint32        `toml:"hold_seconds"`
	ListenPort       int32         `toml:"listen_port"`
	Neighbors        []BGPNeighbor `toml:"neighbors"`
	NeighborsV6      []BGPNeighbor `toml:"neighbors_v6"`
	Announce         BGPAnnounce   `toml:"announce"`
}

// BGPNeighbor identifies a single BGP peer.
type BGPNeighbor struct {
	Address string `toml:"address"`
}

// BGPAnnounce specifies prefixes to originate via BGP.
type BGPAnnounce struct {
	IPv4 []string `toml:"ipv4"`
	IPv6 []string `toml:"ipv6"`
}

// AgentSection holds agent-specific configuration.
type AgentSection struct {
	VsockPort  uint32 `toml:"vsock_port"`
	TCPAddr    string `toml:"tcp_addr"`
	DeployFile string `toml:"deploy_file"`
	LogFile    string `toml:"log_file"`
	Debug      bool   `toml:"debug"`
}

// Config is the single TOML configuration for the mwan monolith.
// All subcommands (agent, watchdog, cutover) read from the same file.
// Default path: /etc/mwan/config.toml, override with --config or MWAN_CONFIG env.
type Config struct {
	Hostname     string `toml:"hostname"`
	MwanVMID     string `toml:"mwan_vmid"`
	MwanMgmtAddr string `toml:"mwan_mgmt_addr"`
	MwanIntIface string `toml:"mwan_int_iface"`

	Email   EmailConfig   `toml:"email"`
	PVE     PVEConfig     `toml:"pve"`
	Network NetworkConfig `toml:"network"`

	Watchdog  WatchdogSection  `toml:"watchdog"`
	Cutover   CutoverSection   `toml:"cutover"`
	Agent     AgentSection     `toml:"agent"`
	BGP       BGPSection       `toml:"bgp"`
	OPNsense  OPNsenseSection  `toml:"opnsense"`
	OOB       OOBSection       `toml:"oob"`
	IfMgr     IfMgrSection     `toml:"ifmgr"`
}

// IfMgrSection holds the mwan ifmgr daemon's role-pluggable configuration.
// Each role is a list of modules (see internal/ifmgr/roles.go); each
// module reads its own sub-config from Modules[name] (a raw map[string]any
// passed verbatim to the module's Constructor).
type IfMgrSection struct {
	Role              string                            `toml:"role"`
	ReconcileInterval string                            `toml:"reconcile_interval"`
	LogFile           string                            `toml:"log_file"`
	JSONLogFile       string                            `toml:"json_log_file"`
	Debug             bool                              `toml:"debug"`
	Iface             map[string]IfMgrIfaceSection      `toml:"iface"`
	Modules           map[string]map[string]any         `toml:"modules"`
}

// IfMgrIfaceSection holds one [ifmgr.iface.<name>] sub-table. The map
// key is the conventional iface name (mbrains, eth0, ...); Name overrides
// it when set explicitly.
type IfMgrIfaceSection struct {
	Name               string `toml:"name"`
	DHCPv4             bool   `toml:"dhcp_v4"`
	RASolicit          bool   `toml:"ra_solicit"`
	DHCPInitialBackoff string `toml:"dhcp_initial_backoff"`
	DHCPMaxBackoff     string `toml:"dhcp_max_backoff"`
}

// OOBSection holds the mbrains/OOB management daemon configuration.
type OOBSection struct {
	MbrainsIface       string `toml:"mbrains_iface"`         // Interface name; e.g. "mbrains"
	OOBTableID         int    `toml:"oob_table_id"`          // /etc/iproute2/rt_tables entry id; e.g. 500
	OOBTableName       string `toml:"oob_table_name"`        // Table name as it appears in rt_tables; e.g. "oob"
	OOBV6Addr          string `toml:"oob_v6_addr"`           // Static OOB v6 address (CIDR); e.g. "3d06:bad:b01:ff::1/128"
	CloudflaredUID     int    `toml:"cloudflared_uid"`       // System uid for cloudflared-oob; rule selects this
	OOBSrcRulePriority int    `toml:"oob_src_rule_priority"` // Priority of the from-:ff::1 rule
	OOBUIDRulePriority int    `toml:"oob_uid_rule_priority"` // Priority of the uid 997 rule
	ReconcileInterval  string `toml:"reconcile_interval"`    // e.g. "60s"
	RASolicitTimeout   string `toml:"ra_solicit_timeout"`    // wait for RA after rdisc6
	RALostAlertAfter   string `toml:"ra_lost_alert_after"`   // alert if RA-learned default missing this long
	DHCPInitialBackoff string `toml:"dhcp_initial_backoff"`  // e.g. "5s"
	DHCPMaxBackoff     string `toml:"dhcp_max_backoff"`      // e.g. "5m"
	LogFile            string `toml:"log_file"`              // text log
	JSONLogFile        string `toml:"json_log_file"`         // structured log
	Debug              bool   `toml:"debug"`                 // enables LevelDebug everywhere
	DryRun             bool   `toml:"dry_run"`               // log intents, do not apply
}

func defaultConfig() Config {
	return Config{
		Email: EmailConfig{MinLevel: "ERROR", Cooldown: "5m"},
		PVE:   PVEConfig{BaseURL: "https://127.0.0.1:8006/api2/json"},
		Network: NetworkConfig{
			PingTargetIPv4: "1.1.1.1",
			PingTargetIPv6: "2606:4700:4700::1111",
			LastDeployPath: "/var/run/mwan-last-deploy",
			LastChangePath: "/var/run/mwan-last-change",
		},
		Watchdog: WatchdogSection{
			DeployWindowMinutes: 30, ConnectivityTimeoutSeconds: 60,
			CheckIntervalHealthy: 30, CheckIntervalDegraded: 10,
			PostRollbackGraceSeconds: 120, AlertCooldownSeconds: 300,
			DeployGracePeriodSeconds: 60, MaxRollbackAttempts: 3,
			SnapshotHealthyThreshold: 20, MaxKnownGoodSnapshots: 3,
			HashCheckEveryNHealthy: 10, MinSnapshotIntervalSeconds: 300,
			MaxTotalSnapshots: 15,
			LogFile:           "/var/log/mwan-watchdog.log", JSONLogFile: "/var/log/mwan-watchdog.jsonl",
			RollbackStateFile: "/run/mwan-rollback.state",
			RollbackLockFile:  "/run/mwan-watchdog-rollback.lock",
			ServiceName:       "mwan-watchdog",
		},
		Cutover: CutoverSection{
			VRID: 51, MasterPriority: 150, BackupPriority: 50, AdvertInterval: 1,
			HealthCheckInterval: 10, HealthCheckWeight: -110,
			HealthCheckFall: 3, HealthCheckRise: 2,
			SSHTimeoutSec: 10, VerifyTimeoutSec: 30, BootWaitSec: 35,
		},
		Agent: AgentSection{
			VsockPort: 50051, TCPAddr: "[::]:50052",
			DeployFile: "/var/run/mwan-last-deploy", LogFile: "/var/log/mwan-agent.log",
		},
		BGP: BGPSection{},
		OOB: OOBSection{
			MbrainsIface:       "mbrains",
			OOBTableID:         500,
			OOBTableName:       "oob",
			OOBV6Addr:          "3d06:bad:b01:ff::1/128",
			CloudflaredUID:     997,
			OOBSrcRulePriority: 6,
			OOBUIDRulePriority: 5,
			ReconcileInterval:  "60s",
			RASolicitTimeout:   "10s",
			RALostAlertAfter:   "5m",
			DHCPInitialBackoff: "5s",
			DHCPMaxBackoff:     "5m",
			LogFile:            "/var/log/mwan-oob.log",
			JSONLogFile:        "/var/log/mwan-oob.jsonl",
		},
	}
}

// Load loads the single TOML config.
// Path: --config flag > MWAN_CONFIG env > /etc/mwan/config.toml
func Load() (*Config, error) {
	path := "/etc/mwan/config.toml"
	if v := os.Getenv("MWAN_CONFIG"); v != "" {
		path = v
	}
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			path = os.Args[i+1]
			os.Args = append(os.Args[:i], os.Args[i+2:]...)
			break
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Env overrides for secrets
	if v := strings.TrimSpace(os.Getenv("SMTP2GO_API_KEY")); v != "" {
		cfg.Email.SMTP2GOAPIKey = v
	}
	if v := strings.TrimSpace(os.Getenv("PVE_TOKEN_SECRET")); v != "" {
		cfg.PVE.TokenSecret = v
	}
	if v := strings.TrimSpace(os.Getenv("OPNSENSE_API_SECRET")); v != "" {
		cfg.OPNsense.APISecret = v
	}

	return &cfg, nil
}

// Validate validates the Config for a specific subcommand.
func Validate(cfg *Config, sub string, dryRun bool) error {
	if cfg.Hostname == "" {
		return errors.New("hostname is required")
	}
	switch sub {
	case "watchdog":
		return validateWatchdog(cfg, dryRun)
	case "cutover":
		return validateCutover(cfg, dryRun)
	case "agent":
		return validateAgent(cfg)
	case "oob":
		return validateOOB(cfg)
	case "ifmgr":
		return validateIfMgr(cfg)
	}
	return nil
}

// validateIfMgr sanity-checks the [ifmgr] section. Module-specific
// validation lives in the module Constructor (Init); here we only catch
// gross structural issues so the CLI fails fast instead of surfacing a
// confusing runtime error from a module Init far down the call stack.
func validateIfMgr(cfg *Config) error {
	if cfg.IfMgr.Role == "" {
		return errors.New("ifmgr.role is required (or pass --role on the CLI)")
	}
	if len(cfg.IfMgr.Iface) == 0 {
		return errors.New("ifmgr: at least one [ifmgr.iface.<name>] section is required")
	}
	if len(cfg.IfMgr.Iface) > 1 {
		return errors.New("ifmgr: multi-iface configurations are not yet supported")
	}
	if cfg.IfMgr.ReconcileInterval != "" {
		if _, err := time.ParseDuration(cfg.IfMgr.ReconcileInterval); err != nil {
			return fmt.Errorf("ifmgr.reconcile_interval %q: %w", cfg.IfMgr.ReconcileInterval, err)
		}
	}
	for name, iface := range cfg.IfMgr.Iface {
		if iface.DHCPInitialBackoff != "" {
			if _, err := time.ParseDuration(iface.DHCPInitialBackoff); err != nil {
				return fmt.Errorf("ifmgr.iface.%s.dhcp_initial_backoff %q: %w", name, iface.DHCPInitialBackoff, err)
			}
		}
		if iface.DHCPMaxBackoff != "" {
			if _, err := time.ParseDuration(iface.DHCPMaxBackoff); err != nil {
				return fmt.Errorf("ifmgr.iface.%s.dhcp_max_backoff %q: %w", name, iface.DHCPMaxBackoff, err)
			}
		}
	}
	return nil
}

func validateOOB(cfg *Config) error {
	if cfg.OOB.MbrainsIface == "" {
		return errors.New("oob.mbrains_iface is required")
	}
	// Modern Linux supports 32-bit routing table IDs. Reserve 0 (unspec) and
	// the well-known IDs (253 local, 254 main, 255 default).
	if cfg.OOB.OOBTableID <= 0 || cfg.OOB.OOBTableID >= 253 && cfg.OOB.OOBTableID <= 255 {
		return fmt.Errorf("oob.oob_table_id %d invalid (must be > 0 and not in [253,255])", cfg.OOB.OOBTableID)
	}
	if cfg.OOB.OOBTableName == "" {
		return errors.New("oob.oob_table_name is required")
	}
	if cfg.OOB.OOBV6Addr == "" {
		return errors.New("oob.oob_v6_addr is required")
	}
	if cfg.OOB.CloudflaredUID <= 0 {
		return errors.New("oob.cloudflared_uid must be > 0")
	}
	if cfg.OOB.OOBSrcRulePriority <= 0 || cfg.OOB.OOBUIDRulePriority <= 0 {
		return errors.New("oob.oob_src_rule_priority and oob.oob_uid_rule_priority must be > 0")
	}
	if cfg.OOB.OOBSrcRulePriority == cfg.OOB.OOBUIDRulePriority {
		return errors.New("oob.oob_src_rule_priority and oob.oob_uid_rule_priority must differ")
	}
	for name, val := range map[string]string{
		"reconcile_interval":   cfg.OOB.ReconcileInterval,
		"ra_solicit_timeout":   cfg.OOB.RASolicitTimeout,
		"ra_lost_alert_after":  cfg.OOB.RALostAlertAfter,
		"dhcp_initial_backoff": cfg.OOB.DHCPInitialBackoff,
		"dhcp_max_backoff":     cfg.OOB.DHCPMaxBackoff,
	} {
		if val == "" {
			return fmt.Errorf("oob.%s is required", name)
		}
		if _, err := time.ParseDuration(val); err != nil {
			return fmt.Errorf("oob.%s: invalid duration %q: %w", name, val, err)
		}
	}
	return nil
}

func validateWatchdog(cfg *Config, dryRun bool) error {
	if cfg.MwanVMID == "" {
		return errors.New("mwan_vmid is required")
	}
	if !dryRun && cfg.Email.SMTP2GOAPIKey == "" {
		return errors.New("[email] smtp2go_api_key is required (set in TOML or SMTP2GO_API_KEY env)")
	}
	if cfg.PVE.TokenID != "" && cfg.PVE.TokenSecret == "" {
		return errors.New("[pve] token_id set but token_secret empty")
	}
	if len(cfg.Network.WANInterfaces) == 0 {
		return errors.New("[network] wan_interfaces must not be empty")
	}
	return nil
}

func validateCutover(cfg *Config, dryRun bool) error {
	if cfg.MwanVMID == "" {
		return errors.New("mwan_vmid is required")
	}
	if !dryRun && cfg.Email.SMTP2GOAPIKey == "" {
		return errors.New("[email] smtp2go_api_key is required (set in TOML or SMTP2GO_API_KEY env)")
	}
	if cfg.MwanMgmtAddr == "" {
		return errors.New("mwan_mgmt_addr is required")
	}
	if cfg.MwanIntIface == "" {
		return errors.New("mwan_int_iface is required")
	}
	if cfg.Cutover.VIPIPv6 == "" {
		return errors.New("[cutover] vip_ipv6 is required")
	}
	if cfg.Cutover.FailoverLXCID == "" {
		return errors.New("[cutover] failover_lxc_id is required")
	}
	if cfg.Email.AlertEmail == "" {
		return errors.New("[email] alert_email is required")
	}
	if cfg.Email.From == "" {
		return errors.New("[email] from is required")
	}
	if cfg.Email.SubjectPrefix == "" {
		return errors.New("[email] subject_prefix is required")
	}
	if cfg.OPNsense.URL == "" {
		return errors.New("[opnsense] url is required")
	}
	return nil
}

func validateAgent(cfg *Config) error {
	if cfg.BGP.Enabled {
		return validateBGP(&cfg.BGP)
	}
	return nil
}

func validateBGP(b *BGPSection) error {
	if b.ASN == 0 {
		return errors.New("[bgp] asn is required")
	}
	if b.RouterID == "" {
		return errors.New("[bgp] router_id is required")
	}
	if b.ListenPort == 0 {
		return errors.New("[bgp] listen_port is required")
	}
	if b.KeepaliveSeconds == 0 {
		return errors.New("[bgp] keepalive_seconds is required")
	}
	if b.HoldSeconds == 0 {
		return errors.New("[bgp] hold_seconds is required")
	}
	if b.HoldSeconds < 3*b.KeepaliveSeconds {
		return fmt.Errorf("[bgp] hold_seconds (%d) must be >= 3 * keepalive_seconds (%d)", b.HoldSeconds, b.KeepaliveSeconds)
	}
	if len(b.Neighbors) == 0 && len(b.NeighborsV6) == 0 {
		return errors.New("[bgp] at least one neighbor (v4 or v6) is required")
	}
	if len(b.Announce.IPv4) == 0 && len(b.Announce.IPv6) == 0 {
		return errors.New("[bgp.announce] at least one prefix (ipv4 or ipv6) is required")
	}
	return nil
}
