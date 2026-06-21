package config

// IfMgrModulesSection is the explicit TOML schema for [ifmgr.modules].
// Each field maps to one supported module table.
type IfMgrModulesSection struct {
	WG                *IfMgrWGHealthSection          `toml:"wg"`
	OOBV6             *IfMgrOOBV6Section             `toml:"oobv6"`
	OOBV4             *IfMgrOOBV4Section             `toml:"oobv4"`
	SLAACHealth       *IfMgrSLAACHealthSection       `toml:"slaac_health"`
	RALost            *IfMgrRALostSection            `toml:"ra_lost"`
	ConnectivityProbe *IfMgrConnectivityProbeSection `toml:"connectivity_probe"`
	BridgeProbe       *IfMgrBridgeProbeSection       `toml:"bridge_probe"`
	CloudflaredTap    *IfMgrCloudflaredTapSection    `toml:"cloudflared_tap"`
	MainV4            *IfMgrMainV4Section            `toml:"mainv4"`
	PolicyRules       *IfMgrPolicyRulesSection       `toml:"policy_rules"`
	HostIPv6Policy    *IfMgrHostIPv6PolicySection    `toml:"host_ipv6_policy"`
	WANRoutes         *IfMgrWANRoutesSection         `toml:"wan_routes"`
}

// IfMgrWGHealthSection configures the WireGuard health probe module.
type IfMgrWGHealthSection struct {
	SSHHost           string   `toml:"ssh_host"`
	SSHPort           *int     `toml:"ssh_port"`
	IdentityFile      string   `toml:"identity_file"`
	Iface             string   `toml:"iface"`
	Sudo              bool     `toml:"sudo"`
	WarnHandshakeAge  string   `toml:"warn_handshake_age"`
	ErrorHandshakeAge string   `toml:"error_handshake_age"`
	Timeout           string   `toml:"timeout"`
	IgnorePeers       []string `toml:"ignore_peers"`
}

// IfMgrOOBV6Section configures IPv6 out-of-band routing state.
type IfMgrOOBV6Section struct {
	Iface                 string `toml:"iface"`
	OOBAddr               string `toml:"oob_addr"`
	OOBTableID            int    `toml:"oob_table_id"`
	ManageSLAACSourceRule *bool  `toml:"manage_slaac_source_rule"`
	SLAACRulePriority     *int   `toml:"slaac_rule_priority"`
}

// IfMgrOOBV4Section configures IPv4 out-of-band routing state.
type IfMgrOOBV4Section struct {
	Iface      string `toml:"iface"`
	OOBTableID int    `toml:"oob_table_id"`
}

// IfMgrSLAACHealthSection configures SLAAC health monitoring.
type IfMgrSLAACHealthSection struct {
	Iface             string   `toml:"iface"`
	DegradedAfter     string   `toml:"degraded_after"`
	EscalateAfter     string   `toml:"escalate_after"`
	AlertAfter        string   `toml:"alert_after"`
	MaxTogglesPerHour *int     `toml:"max_toggles_per_hour"`
	ProbeTargetsV6    []string `toml:"probe_targets_v6"`
	ProbeTimeout      string   `toml:"probe_timeout"`
}

// IfMgrRALostSection configures router advertisement loss detection.
type IfMgrRALostSection struct {
	Iface            string `toml:"iface"`
	RALostAlertAfter string `toml:"ra_lost_alert_after"`
}

// IfMgrConnectivityProbeSection configures the IPv6 connectivity probe module.
type IfMgrConnectivityProbeSection struct {
	Iface          string   `toml:"iface"`
	TargetsV6      []string `toml:"targets_v6"`
	Timeout        string   `toml:"timeout"`
	UnhealthyAfter string   `toml:"unhealthy_after"`
}

// IfMgrBridgeProbeSection configures the bridge carrier probe module.
type IfMgrBridgeProbeSection struct {
	Iface              string `toml:"iface"`
	NoSignalAlertAfter string `toml:"no_signal_alert_after"`
}

// IfMgrCloudflaredTapSection configures cloudflared journal scanning.
type IfMgrCloudflaredTapSection struct {
	Unit              string   `toml:"unit"`
	DowngradePatterns []string `toml:"downgrade_patterns"`
	JournalctlPath    string   `toml:"journalctl_path"`
}

// IfMgrMainV4Section configures the primary IPv4 uplink module.
type IfMgrMainV4Section struct {
	Iface string `toml:"iface"`
}

// IfMgrPolicyRulesSection configures the policy-rules module.
type IfMgrPolicyRulesSection struct {
	Rule []IfMgrPolicyRuleSection `toml:"rule"`
}

// IfMgrWANRoutesSection is the explicit TOML schema for
// [ifmgr.modules.wan_routes].
type IfMgrWANRoutesSection struct {
	InternalIface   string                     `toml:"internal_iface"`
	OpnsenseWanLL   string                     `toml:"opnsense_wan_ll"`
	OpnsenseEdgeV6  string                     `toml:"opnsense_edge_v6"`
	InternalPrefix  string                     `toml:"internal_prefix"`
	InternalNetV4   string                     `toml:"internal_net_v4"`
	HealthStateFile string                     `toml:"health_state_file"`
	ShadowMode      bool                       `toml:"shadow_mode"`
	WAN             []IfMgrWANRoutesWANSection `toml:"wan"`
}

// IfMgrWANRoutesWANSection is one [[ifmgr.modules.wan_routes.wan]]
// table in the config file.
type IfMgrWANRoutesWANSection struct {
	Name       string `toml:"name"`
	Iface      string `toml:"iface"`
	TableID    int    `toml:"table_id"`
	FwMark     int    `toml:"fw_mark"`
	FwMarkPrio int    `toml:"fw_mark_prio"`
	FromPrio   int    `toml:"from_prio"`
	NptPrefix  string `toml:"npt_prefix"`
	V4Source   string `toml:"v4_source"`
}

// IfMgrHostIPv6PolicySection is the explicit TOML schema for
// [ifmgr.modules.host_ipv6_policy].
type IfMgrHostIPv6PolicySection struct {
	MissingIfaceGracePeriod string                            `toml:"missing_iface_grace_period"`
	Interface               []IfMgrHostIPv6PolicyIfaceSection `toml:"interface"`
}

// IfMgrHostIPv6PolicyIfaceSection is one [[ifmgr.modules.host_ipv6_policy.interface]]
// table in the config file.
type IfMgrHostIPv6PolicyIfaceSection struct {
	Name             string `toml:"name"`
	AcceptRA         int    `toml:"accept_ra"`
	AutoConf         bool   `toml:"autoconf"`
	AcceptRADefRtr   bool   `toml:"accept_ra_defrtr"`
	SolicitRA        bool   `toml:"solicit_ra"`
	CleanupRADefault bool   `toml:"cleanup_ra_default"`
}

// IfMgrPolicyRuleSection is one [[ifmgr.modules.policy_rules.rule]] table.
type IfMgrPolicyRuleSection struct {
	Family   string `toml:"family"`
	Priority int    `toml:"priority"`
	From     string `toml:"from"`
	UIDRange string `toml:"uid_range"`
	UIDUser  string `toml:"uid_user"`
	Table    string `toml:"table"`
	TableID  int    `toml:"table_id"`
}
