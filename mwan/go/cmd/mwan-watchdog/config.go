package main

import (
	"errors"

	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ---------------------------------------------------------------------------
// networkConfig: topology that can vary per deployment
// ---------------------------------------------------------------------------

// WANInterface describes one WAN uplink inside the MWAN VM.
// The watchdog pings through each interface to determine whether connectivity
// loss is a routing failure or a real ISP outage.
type WANInterface struct {
	// Name is the Linux interface name inside the MWAN VM (e.g. "enwebpass0").
	Name string `toml:"name"`
}

// networkConfig holds all site-specific topology values that belong in a
// config file rather than baked into the binary.  The defaults match the
// current goodkind.io lab; override by writing /etc/mwan-watchdog/network.toml
// (or the path in MWAN_NETWORK_CONFIG).
type networkConfig struct {
	// PingTargetIPv4 is the public IPv4 address used for host-level reachability probes.
	PingTargetIPv4 string `toml:"ping_target_ipv4"`
	// PingTargetIPv6 is the public IPv6 address used for host-level reachability probes.
	PingTargetIPv6 string `toml:"ping_target_ipv6"`
	// WANInterfaces lists all WAN uplinks inside the MWAN VM.
	// The watchdog pings through each one when diagnosing a total connectivity loss.
	// Add new ISPs here without recompiling.
	WANInterfaces []WANInterface `toml:"wan_interfaces"`
	// LastDeployPath is the path inside the MWAN VM that contains the last
	// deploy Unix timestamp (written by the deploy playbook).
	LastDeployPath string `toml:"last_deploy_path"`
	// LastChangePath is updated when any watched config changes (path unit or
	// scripts hook).
	LastChangePath string `toml:"last_change_path"`
	// ConfigHashPath holds a composite sha256 written by mwan-change-detect.
	ConfigHashPath string `toml:"config_hash_path"`
}

func defaultNetworkConfig() networkConfig {
	return networkConfig{
		PingTargetIPv4: "1.1.1.1",
		PingTargetIPv6: "2606:4700:4700::1111",
		WANInterfaces: []WANInterface{
			{Name: "enwebpass0"},
			{Name: "enmbrains0"},
		},
		LastDeployPath: "/var/run/mwan-last-deploy",
		LastChangePath: "/var/run/mwan-last-change",
		ConfigHashPath: "/var/run/mwan-config-hash",
	}
}

// loadNetworkConfig loads a TOML file from path, merging over the defaults.
// If path is empty or the file does not exist, defaults are returned without
// error (the file is optional).
func loadNetworkConfig(path string) (networkConfig, error) {
	nc := defaultNetworkConfig()
	if path == "" {
		return nc, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nc, nil
		}
		return nc, fmt.Errorf("read network config %q: %w", path, err)
	}
	if err := toml.Unmarshal(data, &nc); err != nil {
		return nc, fmt.Errorf("parse network config %q: %w", path, err)
	}
	if len(nc.WANInterfaces) == 0 {
		return nc, fmt.Errorf(
			"network config %q: wan_interfaces must not be empty", path,
		)
	}
	return nc, nil
}

// wanIfaceNames returns just the interface name strings for iteration.
func (nc networkConfig) wanIfaceNames() []string {
	names := make([]string, len(nc.WANInterfaces))
	for i, w := range nc.WANInterfaces {
		names[i] = w.Name
	}
	return names
}

// ---------------------------------------------------------------------------
// config: runtime/env-driven settings
// ---------------------------------------------------------------------------

type config struct {
	MwanVMID                   string
	DeployWindowMinutes        int
	ConnectivityTimeoutSeconds int
	CheckIntervalHealthy       time.Duration
	CheckIntervalDegraded      time.Duration
	PostRollbackGraceSeconds   time.Duration
	LogFile                    string
	JSONLogFile                string
	RollbackStateFile          string
	RollbackLockFile           string
	AlertEmail                 string
	AlertCooldownSeconds       int
	SMTP2GOAPIKey              string
	MaxIterations              int
	VsockCID                   uint32
	VsockPort                  uint32
	MwanAgentTCPAddr           string
	PVEBaseURL                 string
	PVENode                    string
	PVETokenID                 string
	PVESecret                  string
	NetworkConfigPath          string
	EmailVerbosity             EmailVerbosity
	ConfigWarnings             []string

	SnapshotHealthyThreshold   int
	MaxKnownGoodSnapshots      int
	HashCheckEveryNHealthy     int
	MinSnapshotIntervalSeconds int
	MaxTotalSnapshots          int
}

func getenv(key, defaultVal string) string {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	return v
}

func getenvInt(key string, defaultVal int) (int, string) {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal, ""
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return defaultVal, fmt.Sprintf(
			"env %s=%q is not a valid integer; using default %d",
			key, v, defaultVal,
		)
	}
	return n, ""
}

func getenvUint32(key string, defaultVal uint32) (uint32, string) {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal, ""
	}
	n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
	if err != nil {
		return defaultVal, fmt.Sprintf(
			"env %s=%q is not a valid uint32; using default %d",
			key, v, defaultVal,
		)
	}
	return uint32(n), ""
}

func loadConfig(requireAPIKey bool) (config, error) {
	var warnings []string
	appendWarn := func(w string) {
		if w != "" {
			warnings = append(warnings, w)
		}
	}

	deployWin, w := getenvInt("DEPLOY_WINDOW_MINUTES", 30)
	appendWarn(w)
	connTO, w := getenvInt("CONNECTIVITY_TIMEOUT_SECONDS", 30)
	appendWarn(w)
	checkHealthy, w := getenvInt("CHECK_INTERVAL_HEALTHY", 30)
	appendWarn(w)
	checkDegraded, w := getenvInt("CHECK_INTERVAL_DEGRADED", 10)
	appendWarn(w)
	postGrace, w := getenvInt("POST_ROLLBACK_GRACE_SECONDS", 120)
	appendWarn(w)
	alertCD, w := getenvInt("ALERT_COOLDOWN_SECONDS", 300)
	appendWarn(w)
	snapHealthy, w := getenvInt("SNAPSHOT_HEALTHY_THRESHOLD", 20)
	appendWarn(w)
	maxKG, w := getenvInt("MAX_KNOWN_GOOD_SNAPSHOTS", 3)
	appendWarn(w)
	hashEvery, w := getenvInt("HASH_CHECK_EVERY_N_HEALTHY", 10)
	appendWarn(w)
	minSnapInt, w := getenvInt("MIN_SNAPSHOT_INTERVAL_SECONDS", 300)
	appendWarn(w)
	maxTotalSnaps, w := getenvInt("MAX_TOTAL_SNAPSHOTS", 15)
	appendWarn(w)
	vsockCID, w := getenvUint32("MWAN_VSOCK_CID", defaultVsockCID)
	appendWarn(w)
	vsockPort, w := getenvUint32("MWAN_VSOCK_PORT", defaultVsockPort)
	appendWarn(w)

	cfg := config{
		MwanVMID:                   getenv("MWAN_VMID", "113"),
		DeployWindowMinutes:        deployWin,
		ConnectivityTimeoutSeconds: connTO,
		CheckIntervalHealthy:       time.Duration(checkHealthy) * time.Second,
		CheckIntervalDegraded:      time.Duration(checkDegraded) * time.Second,
		PostRollbackGraceSeconds:   time.Duration(postGrace) * time.Second,
		LogFile: getenv(
			"LOG_FILE", "/var/log/mwan-watchdog.log",
		),
		JSONLogFile: getenv(
			"LOG_JSON_FILE", "/var/log/mwan-watchdog.jsonl",
		),
		RollbackStateFile: getenv(
			"ROLLBACK_STATE_FILE", "/run/mwan-rollback.state",
		),
		RollbackLockFile: getenv(
			"ROLLBACK_LOCK_FILE",
			"/run/mwan-watchdog-rollback.lock",
		),
		AlertEmail:           getenv("ALERT_EMAIL", "root@localhost"),
		AlertCooldownSeconds: alertCD,
		SMTP2GOAPIKey: strings.TrimSpace(
			os.Getenv("SMTP2GO_API_KEY"),
		),
		VsockCID:  vsockCID,
		VsockPort: vsockPort,
		MwanAgentTCPAddr: getenv(
			"MWAN_AGENT_TCP_ADDR", "[3d06:bad:b01::113]:50052",
		),
		PVEBaseURL: getenv(
			"PVE_BASE_URL", "https://127.0.0.1:8006/api2/json",
		),
		PVENode:    getenv("PVE_NODE", "vault"),
		PVETokenID: getenv("PVE_TOKEN_ID", ""),
		PVESecret:  strings.TrimSpace(os.Getenv("PROXMOX_API_TOKEN")),
		NetworkConfigPath: getenv(
			"MWAN_NETWORK_CONFIG",
			"/etc/mwan-watchdog/network.toml",
		),
		EmailVerbosity:             emailVerbosityFromEnv(),
		ConfigWarnings:             warnings,
		SnapshotHealthyThreshold:   snapHealthy,
		MaxKnownGoodSnapshots:      maxKG,
		HashCheckEveryNHealthy:     hashEvery,
		MinSnapshotIntervalSeconds: minSnapInt,
		MaxTotalSnapshots:          maxTotalSnaps,
	}
	if requireAPIKey && cfg.SMTP2GOAPIKey == "" {
		return config{}, errors.New(
			"SMTP2GO_API_KEY is required (set env or use --dry-run)",
		)
	}
	return cfg, nil
}
