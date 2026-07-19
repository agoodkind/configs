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
	WAN               *IfMgrModulesWANSection        `toml:"wan"`
	NPT               *IfMgrNPTSection               `toml:"npt"`
}

// IfMgrModulesWANSection is the [ifmgr.modules.wan] table. It nests the
// wan.routes module config under the key `routes`, so the module renders as
// [ifmgr.modules.wan.routes].
type IfMgrModulesWANSection struct {
	Routes *IfMgrWANRoutesSection `toml:"routes"`
}

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

type IfMgrOOBV6Section struct {
	Iface                 string `toml:"iface"`
	OOBAddr               string `toml:"oob_addr"`
	OOBTableID            int    `toml:"oob_table_id"`
	ManageSLAACSourceRule *bool  `toml:"manage_slaac_source_rule"`
	SLAACRulePriority     *int   `toml:"slaac_rule_priority"`
}

type IfMgrOOBV4Section struct {
	Iface      string `toml:"iface"`
	OOBTableID int    `toml:"oob_table_id"`
}

type IfMgrSLAACHealthSection struct {
	Iface             string   `toml:"iface"`
	DegradedAfter     string   `toml:"degraded_after"`
	EscalateAfter     string   `toml:"escalate_after"`
	AlertAfter        string   `toml:"alert_after"`
	MaxTogglesPerHour *int     `toml:"max_toggles_per_hour"`
	ProbeTargetsV6    []string `toml:"probe_targets_v6"`
	ProbeTimeout      string   `toml:"probe_timeout"`
}

type IfMgrRALostSection struct {
	Iface            string `toml:"iface"`
	RALostAlertAfter string `toml:"ra_lost_alert_after"`
}

type IfMgrConnectivityProbeSection struct {
	Iface          string   `toml:"iface"`
	TargetsV6      []string `toml:"targets_v6"`
	Timeout        string   `toml:"timeout"`
	UnhealthyAfter string   `toml:"unhealthy_after"`
}

type IfMgrBridgeProbeSection struct {
	Iface              string `toml:"iface"`
	NoSignalAlertAfter string `toml:"no_signal_alert_after"`
}

type IfMgrCloudflaredTapSection struct {
	Unit              string   `toml:"unit"`
	DowngradePatterns []string `toml:"downgrade_patterns"`
	JournalctlPath    string   `toml:"journalctl_path"`
}

type IfMgrMainV4Section struct {
	Iface string `toml:"iface"`
}

type IfMgrPolicyRulesSection struct {
	Rule []IfMgrPolicyRuleSection `toml:"rule"`
}

// IfMgrWANEntry is one [ifmgr.wan.<name>] table: all per-WAN config, keyed by
// WAN name. The map lives on IfMgrSection.WAN (toml:"wan") so it renders as
// keyed sub-tables [ifmgr.wan.<name>], mirroring [ifmgr.iface.<name>]. Each WAN
// has one home here: the interface plus the policy-routing slots wan.routes owns
// (table_id, fw_mark, fw_mark_prio, from_prio, npt_prefix, v4_source). Modules
// read the fields they need; npt uses only iface. The shared internal prefix and
// edge addresses live on [ifmgr] itself (IfMgrSection.InternalPrefix,
// OpnsenseEdgeV6, MwanbrEdgeV6) because a TOML table cannot hold both scalar keys
// and a map of sub-tables.
type IfMgrWANEntry struct {
	Iface      string `toml:"iface"`
	TableID    int    `toml:"table_id"`
	FwMark     int    `toml:"fw_mark"`
	FwMarkPrio int    `toml:"fw_mark_prio"`
	FromPrio   int    `toml:"from_prio"`
	NptPrefix  string `toml:"npt_prefix"`
	V4Source   string `toml:"v4_source"`
}

// IfMgrWANRoutesSection is the explicit TOML schema for
// [ifmgr.modules.wan.routes]. The WAN list, shared prefixes, and per-WAN routing
// data live in [ifmgr.wan.<name>] and on [ifmgr]; this section keeps only the
// module-wide inputs that are not per-WAN.
type IfMgrWANRoutesSection struct {
	InternalIface   string `toml:"internal_iface"`
	OpnsenseWanLL   string `toml:"opnsense_wan_ll"`
	InternalNetV4   string `toml:"internal_net_v4"`
	HealthStateFile string `toml:"health_state_file"`
	ShadowMode      bool   `toml:"shadow_mode"`
}

// IfMgrNPTSection is the explicit TOML schema for [ifmgr.modules.npt]. It
// carries only the module's own shadow toggle; the WAN list, internal prefix,
// and edge addresses come from the shared [ifmgr.wan] section, so they are not
// duplicated here.
type IfMgrNPTSection struct {
	ShadowMode bool `toml:"shadow_mode"`
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

type IfMgrPolicyRuleSection struct {
	Family   string `toml:"family"`
	Priority int    `toml:"priority"`
	From     string `toml:"from"`
	UIDRange string `toml:"uid_range"`
	UIDUser  string `toml:"uid_user"`
	Table    string `toml:"table"`
	TableID  int    `toml:"table_id"`
}
