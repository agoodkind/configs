// Package config loads and validates the shared mwan TOML configuration.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// WanIfaceNames returns the configured WAN interface names in config order.
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

	// ServiceName is the systemd unit name that watchdog subcommands target.
	// Defaults to "mwan-watchdog" on production. Testbed sets this to
	// "mwan-watchdog-testbed" so the same binary works against the
	// per-environment unit.
	ServiceName string `toml:"service_name"`
}

// Duration helper methods for WatchdogSection.

// HealthyInterval returns the steady-state watchdog poll interval.
func (ws WatchdogSection) HealthyInterval() time.Duration {
	return time.Duration(ws.CheckIntervalHealthy) * time.Second
}

// DegradedInterval returns the poll interval used after a failed probe.
func (ws WatchdogSection) DegradedInterval() time.Duration {
	return time.Duration(ws.CheckIntervalDegraded) * time.Second
}

// PostRollbackGrace returns the grace period after a rollback completes.
func (ws WatchdogSection) PostRollbackGrace() time.Duration {
	return time.Duration(ws.PostRollbackGraceSeconds) * time.Second
}

// FailoverSection holds BGP failover configuration.
type FailoverSection struct {
	LXCID string `toml:"lxc_id"`
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

	// SSHUser is the SSH login on OPNsense. OPNsense disables root SSH by
	// default and ships with an admin user that has wheel + NOPASSWD sudo.
	// Defaults to "agoodkind". OPNsense operations that need root are
	// wrapped with "sudo" automatically.
	SSHUser string `toml:"ssh_user"`

	// Host, Probe, Upgrade, Validate, and ConfigImport are the [opnsense.*]
	// subsections for the gRPC-over-virtio-serial transport between the Proxmox
	// host and the OPNsense guest.
	Host         OpnsenseHostSection         `toml:"host"`
	Probe        OpnsenseProbeSection        `toml:"probe"`
	Upgrade      OpnsenseUpgradeSection      `toml:"upgrade"`
	Validate     OpnsenseValidateSection     `toml:"validate"`
	ConfigImport OpnsenseConfigImportSection `toml:"config_import"`
}

// OpnsenseConfigImportSection configures the `mwan opnsense config import`
// verb. Substitutions is the YAML path describing the find/replace rules
// applied to the redacted prod XML, and Output is where the transformed
// XML lands. The SOURCE argument is positional on the command line; only
// Substitutions and Output are operator-tunable enough to live in TOML.
type OpnsenseConfigImportSection struct {
	Substitutions string `toml:"substitutions"`
	Output        string `toml:"output"`
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

// OpnsenseHostSection configures the mwan-opnsense-host daemon that runs
// on the Proxmox host. Duration fields use the IfMgr style (string parsed
// at use site via [time.ParseDuration]) so the wire format matches the
// rest of the file.
type OpnsenseHostSection struct {
	Upstream                  string `toml:"upstream"`
	Listen                    string `toml:"listen"`
	ReconnectDuration         string `toml:"reconnect"`
	HeartbeatIntervalDuration string `toml:"heartbeat_interval"`
	HeartbeatTimeoutDuration  string `toml:"heartbeat_timeout"`
}

// OpnsenseProbeSection configures the mwan-probe client that talks to
// the host daemon over the local Unix socket.
type OpnsenseProbeSection struct {
	Target           string `toml:"target"`
	TimeoutDuration  string `toml:"timeout"`
	UploadChunkBytes int    `toml:"upload_chunk_bytes"`
}

// OpnsenseUpgradeSection configures the mwan upgrade orchestrator. Operator
// tunables live here. EnvTransport is retained for forward compatibility, and
// the CLI currently uses the gRPC path.
type OpnsenseUpgradeSection struct {
	VMID                     int    `toml:"vmid"`
	EnvTransport             string `toml:"env_transport"`
	EnvGRPCTarget            string `toml:"env_grpc_target"`
	StateDir                 string `toml:"state_dir"`
	ExecTimeoutDuration      string `toml:"exec_timeout"`
	UpgradeTimeoutDuration   string `toml:"upgrade_timeout"`
	PostRollbackWaitDuration string `toml:"post_rollback_wait"`
	OPNsenseSSH              string `toml:"opnsense_ssh"`
	OPNsenseJump             string `toml:"opnsense_jump"`
	ProxmoxSSH               string `toml:"proxmox_ssh"`
	LANClientSSH             string `toml:"lan_client_ssh"`
	OPNsenseAddr             string `toml:"opnsense_addr"`

	// Target is the OPNsense version the upgrade is heading toward
	// (e.g. "26.7"). It is optional; phases like prepare/snapshot work
	// without it, and execute/validate read it when present.
	Target string `toml:"target"`

	// DryRunExecute swaps the real upgrade for `opnsense-update -c`.
	DryRunExecute bool `toml:"dry_run_execute"`

	// UseBootEnvironment requests a bectl boot-environment alongside
	// the snapshot.
	UseBootEnvironment bool `toml:"use_boot_environment"`

	// AcceptPartial treats a partial-pass validate as a manual-decision
	// state instead of failing the phase outright.
	AcceptPartial bool `toml:"accept_partial"`

	// KeepSnapshot retains the upgrade snapshot during commit; gc sweeps
	// it later.
	KeepSnapshot bool `toml:"keep_snapshot"`

	// GCOlderThan is the gc age threshold.
	GCOlderThan string `toml:"gc_older_than"`

	// ResetConfirm gates the reset phase's apply path. When false (the
	// default), reset prints the plan and exits with 2 so the operator
	// can review it; when true, reset applies the plan via
	// upgrade.ResetExecute.
	ResetConfirm bool `toml:"reset_confirm"`

	// DiffAgainst is an optional path to a baseline JSON file. When
	// non-empty, the validate phase diffs the freshly captured baseline
	// against it via validate.Diff and prints the report.
	DiffAgainst string `toml:"diff_against"`

	// Validate is the inlined validator subsection so the upgrade
	// orchestrator can drive the same matrix as the validate verb
	// without duplicating every field.
	Validate OpnsenseUpgradeValidateSection `toml:"validate"`
}

// OpnsenseUpgradeValidateSection holds the validator inputs the upgrade
// phases share with the standalone validate verb.
type OpnsenseUpgradeValidateSection struct {
	APIKey               string `toml:"api_key"`
	APISecret            string `toml:"api_secret"`
	BGPv4Neighbors       string `toml:"bgp_v4_neighbors"`
	BGPv6Neighbors       string `toml:"bgp_v6_neighbors"`
	OPNsenseLAN          string `toml:"opnsense_lan"`
	MWANOpnsenseSocket   string `toml:"mwan_opnsense_socket"`
	MWANOpnsenseHostSock string `toml:"mwan_opnsense_host_socket"`
	SettleAfterUpgrade   string `toml:"settle_after_upgrade"`
}

// OpnsenseValidateSection configures the standalone validate verb. The
// CLI surface accepts no flags; every input lives here.
type OpnsenseValidateSection struct {
	EnvTransport         string `toml:"env_transport"`
	EnvGRPCTarget        string `toml:"env_grpc_target"`
	StateDir             string `toml:"state_dir"`
	OPNsenseSSH          string `toml:"opnsense_ssh"`
	OPNsenseJump         string `toml:"opnsense_jump"`
	ProxmoxSSH           string `toml:"proxmox_ssh"`
	LANClientSSH         string `toml:"lan_client_ssh"`
	OPNsenseAddr         string `toml:"opnsense_addr"`
	APIKey               string `toml:"api_key"`
	APISecret            string `toml:"api_secret"`
	BGPv4Neighbors       string `toml:"bgp_v4_neighbors"`
	BGPv6Neighbors       string `toml:"bgp_v6_neighbors"`
	OPNsenseLAN          string `toml:"opnsense_lan"`
	MWANOpnsenseSocket   string `toml:"mwan_opnsense_socket"`
	MWANOpnsenseHostSock string `toml:"mwan_opnsense_host_socket"`
	SettleAfterUpgrade   string `toml:"settle_after_upgrade"`
	Timeout              string `toml:"timeout"`
}

// BGPSection holds embedded GoBGP speaker configuration.
type BGPSection struct {
	Enabled          bool               `toml:"enabled"`
	ASN              uint32             `toml:"asn"`
	RouterID         string             `toml:"router_id"`
	NextHopV6        string             `toml:"next_hop_v6"` // IPv6 next-hop for announced routes (optional, defaults to RouterID)
	KeepaliveSeconds uint32             `toml:"keepalive_seconds"`
	HoldSeconds      uint32             `toml:"hold_seconds"`
	ListenPort       int32              `toml:"listen_port"`
	Neighbors        []BGPNeighbor      `toml:"neighbors"`
	NeighborsV6      []BGPNeighbor      `toml:"neighbors_v6"`
	Announce         BGPAnnounce        `toml:"announce"`
	GracefulRestart  BGPGracefulRestart `toml:"graceful_restart"`
}

// BGPGracefulRestart configures BGP Graceful Restart (RFC 4724) on the
// embedded GoBGP speaker. When Enabled is true, the speaker negotiates
// the GR capability with each peer so that planned restarts of mwan-agent
// do not trigger immediate route withdrawal on the helper (OPNsense FRR).
//
// RestartTime is advertised to the helper as the maximum number of seconds
// the helper should hold our routes after the BGP session drops.
// NotificationEnabled mirrors the "N" bit (RFC 8538) so a
// NOTIFICATION-triggered session reset still preserves the GR semantics.
type BGPGracefulRestart struct {
	Enabled             bool   `toml:"enabled"`
	RestartTime         uint32 `toml:"restart_time"`
	NotificationEnabled bool   `toml:"notification_enabled"`
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
//
// DeployExpected gates the "deploy-file-missing" warning emitted by
// GetConfigState when DeployFile cannot be read. The default is true,
// which preserves the historical behaviour for production and testbed
// hosts that are deployed by Ansible. Fresh hosts that have never been
// deployed should set this to false so the missing file is treated as
// steady state and no notify event is fired.
type AgentSection struct {
	VsockPort      uint32 `toml:"vsock_port"`
	TCPAddr        string `toml:"tcp_addr"`
	DeployFile     string `toml:"deploy_file"`
	DeployExpected bool   `toml:"deploy_expected"`
	LogFile        string `toml:"log_file"`
	Debug          bool   `toml:"debug"`
}

// Config is the single TOML configuration for the mwan monolith.
// Default path: /etc/mwan/config.toml, override with --config or MWAN_CONFIG env.
type Config struct {
	Hostname     string `toml:"hostname"`
	MwanVMID     string `toml:"mwan_vmid"`
	MwanMgmtAddr string `toml:"mwan_mgmt_addr"`

	Email   EmailConfig   `toml:"email"`
	PVE     PVEConfig     `toml:"pve"`
	Network NetworkConfig `toml:"network"`

	Watchdog WatchdogSection `toml:"watchdog"`
	Failover FailoverSection `toml:"failover"`
	Agent    AgentSection    `toml:"agent"`
	BGP      BGPSection      `toml:"bgp"`
	OPNsense OPNsenseSection `toml:"opnsense"`
	IfMgr    IfMgrSection    `toml:"ifmgr"`
	Notify   NotifySection   `toml:"notify"`
}

// NotifySection controls the per-(kind, key) repeat cadence that the
// internal/notify Manager applies on top of the state-change boundary.
// The shape mirrors IfMgrAlertsSection because slice A introduces the
// notify package by carving the state machine out of internal/ifmgr;
// callers wired to the new package read cfg.Notify, callers still
// wired to the ifmgr AlertManager continue to read cfg.IfMgr.Alerts
// until slice B migrates them.
//
// RepeatEvery is the global default applied to every alert kind unless
// overridden in PerKind. PerKind is keyed by alert kind (the Kind field
// on notify.Event), not by module name. Both fields accept Go duration
// strings like "30m" or "24h".
//
// Default behaviour with empty values: RepeatEvery == "0s" means
// alerts fire once per transition only and never repeat.
type NotifySection struct {
	RepeatEvery string            `toml:"repeat_every"`
	PerKind     map[string]string `toml:"per_kind"`
}

// IfMgrSection holds the mwan ifmgr daemon's role-pluggable configuration.
// Each role is a list of modules (see internal/ifmgr/roles.go), and the
// module config schema is explicitly modeled in IfMgrModulesSection.
type IfMgrSection struct {
	Role              string                       `toml:"role"`
	ReconcileInterval string                       `toml:"reconcile_interval"`
	LogFile           string                       `toml:"log_file"`
	JSONLogFile       string                       `toml:"json_log_file"`
	Debug             bool                         `toml:"debug"`
	Iface             map[string]IfMgrIfaceSection `toml:"iface"`
	Modules           IfMgrModulesSection          `toml:"modules"`
	Alerts            IfMgrAlertsSection           `toml:"alerts"`
}

// IfMgrAlertsSection controls the per-alert repeat cadence for the
// AlertManager. RepeatEvery is the global default applied to every alert
// kind unless overridden in PerModule. PerModule is keyed by alert kind
// (the first arg to AlertManager.Notify, e.g. "wg-peer-stalled" or
// "wg-reconcile-failed"), not by module name. Keying on alert kind lets
// one module emit multiple kinds and get a separate cadence for each.
//
// Default behaviour with empty values: RepeatEvery == "0s" means alerts
// fire once per transition only and never repeat.
type IfMgrAlertsSection struct {
	RepeatEvery string            `toml:"repeat_every"`
	PerModule   map[string]string `toml:"per_module"`
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

func defaultConfig() Config {
	cfg := defaultConfigBase()
	// Populate the [opnsense.*] subsections outside the base Config literal.
	cfg.OPNsense.Host = OpnsenseHostSection{
		Upstream:                  "unix:///var/run/qemu-server/101.mwanrpc",
		Listen:                    "/var/run/mwan-opnsense.sock",
		ReconnectDuration:         "2s",
		HeartbeatIntervalDuration: "30s",
		HeartbeatTimeoutDuration:  "10s",
	}
	cfg.OPNsense.Probe = OpnsenseProbeSection{
		Target:           "unix:///var/run/mwan-opnsense.sock",
		TimeoutDuration:  "10s",
		UploadChunkBytes: 16384,
	}
	cfg.OPNsense.Upgrade = OpnsenseUpgradeSection{
		VMID:                     101,
		EnvTransport:             "grpc",
		EnvGRPCTarget:            "unix:///var/run/mwan-opnsense.sock",
		StateDir:                 "/var/lib/mwan/upgrades",
		ExecTimeoutDuration:      "60m",
		UpgradeTimeoutDuration:   "30m",
		PostRollbackWaitDuration: "5m",
		OPNsenseSSH:              "",
		OPNsenseJump:             "",
		ProxmoxSSH:               "",
		LANClientSSH:             "",
		OPNsenseAddr:             "",
		Target:                   "",
		DryRunExecute:            false,
		UseBootEnvironment:       false,
		AcceptPartial:            false,
		KeepSnapshot:             false,
		GCOlderThan:              "168h",
		ResetConfirm:             false,
		DiffAgainst:              "",
		Validate: OpnsenseUpgradeValidateSection{
			APIKey:               "",
			APISecret:            "",
			BGPv4Neighbors:       "",
			BGPv6Neighbors:       "",
			OPNsenseLAN:          "",
			MWANOpnsenseSocket:   "",
			MWANOpnsenseHostSock: "",
			SettleAfterUpgrade:   "5m",
		},
	}
	cfg.OPNsense.Validate = OpnsenseValidateSection{
		EnvTransport:         "grpc",
		EnvGRPCTarget:        "unix:///var/run/mwan-opnsense.sock",
		StateDir:             "/var/lib/mwan/upgrades",
		OPNsenseSSH:          "",
		OPNsenseJump:         "",
		ProxmoxSSH:           "",
		LANClientSSH:         "",
		OPNsenseAddr:         "",
		APIKey:               "",
		APISecret:            "",
		BGPv4Neighbors:       "",
		BGPv6Neighbors:       "",
		OPNsenseLAN:          "",
		MWANOpnsenseSocket:   "",
		MWANOpnsenseHostSock: "",
		SettleAfterUpgrade:   "5m",
		Timeout:              "10m",
	}
	cfg.OPNsense.ConfigImport = OpnsenseConfigImportSection{
		Substitutions: "",
		Output:        "",
	}
	return cfg
}

func defaultConfigBase() Config {
	// Assign field-by-field instead of via a struct literal so the
	// exhaustruct lint does not require enumerating every zero-value
	// sub-section (OPNsense, IfMgr, and their many nested sub-structs).
	var cfg Config
	cfg.Email.MinLevel = "ERROR"
	cfg.Email.Cooldown = "5m"
	cfg.PVE.BaseURL = "https://127.0.0.1:8006/api2/json"
	cfg.Network.PingTargetIPv4 = "1.1.1.1"
	cfg.Network.PingTargetIPv6 = "2606:4700:4700::1111"
	cfg.Network.LastDeployPath = "/var/lib/mwan/last-deploy"
	cfg.Network.LastChangePath = "/var/run/mwan-last-change"
	cfg.Watchdog.DeployWindowMinutes = 30
	cfg.Watchdog.ConnectivityTimeoutSeconds = 60
	cfg.Watchdog.CheckIntervalHealthy = 30
	cfg.Watchdog.CheckIntervalDegraded = 10
	cfg.Watchdog.PostRollbackGraceSeconds = 120
	cfg.Watchdog.AlertCooldownSeconds = 300
	cfg.Watchdog.DeployGracePeriodSeconds = 60
	cfg.Watchdog.MaxRollbackAttempts = 3
	cfg.Watchdog.SnapshotHealthyThreshold = 20
	cfg.Watchdog.MaxKnownGoodSnapshots = 3
	cfg.Watchdog.HashCheckEveryNHealthy = 10
	cfg.Watchdog.MinSnapshotIntervalSeconds = 300
	cfg.Watchdog.MaxTotalSnapshots = 15
	cfg.Watchdog.LogFile = "/var/log/mwan-watchdog.log"
	cfg.Watchdog.JSONLogFile = "/var/log/mwan-watchdog.jsonl"
	cfg.Watchdog.RollbackStateFile = "/run/mwan-rollback.state"
	cfg.Watchdog.RollbackLockFile = "/run/mwan-watchdog-rollback.lock"
	cfg.Watchdog.ServiceName = "mwan-watchdog"
	cfg.Agent.VsockPort = 50051
	cfg.Agent.TCPAddr = "[::]:50052"
	cfg.Agent.DeployFile = "/var/lib/mwan/last-deploy"
	cfg.Agent.DeployExpected = true
	cfg.Agent.LogFile = "/var/log/mwan-agent.log"
	cfg.BGP.GracefulRestart.Enabled = true
	cfg.BGP.GracefulRestart.RestartTime = 30
	cfg.BGP.GracefulRestart.NotificationEnabled = true
	return cfg
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

	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		slog.Error("read config failed", "path", cleanPath, "error", err)
		return nil, fmt.Errorf("read config %s: %w", cleanPath, err)
	}

	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		slog.Error("parse config failed", "path", cleanPath, "error", err)
		return nil, fmt.Errorf("parse config %s: %w", cleanPath, err)
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
