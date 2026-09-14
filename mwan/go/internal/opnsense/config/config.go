// Package config loads the OPNsense tooling's own TOML configuration: the
// Proxmox-host bridge, the chardev drainer, the probe client, the upgrade
// orchestrator, the validate verb, and the config import verb.
//
// The file keeps the [opnsense.*] table names the gateway configuration used,
// so the same decoder reads both the tooling's own file and the gateway's
// /etc/mwan/config.toml. The in-guest daemon reads daemoncfg, not this file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// DefaultPath is where the Proxmox host deploy installs this file.
	DefaultPath = "/etc/opnsensectl/config.toml"
	// LegacyPath is the gateway configuration that carried the [opnsense.*]
	// tables before DefaultPath existed. Load reads it only while DefaultPath
	// is absent, so a binary that reaches a host before the deploy renders
	// DefaultPath still finds the values that host was rendered with.
	LegacyPath = "/etc/mwan/config.toml"
	// PathEnv names one file that replaces both DefaultPath and LegacyPath.
	PathEnv = "OPNSENSECTL_CONFIG"
)

// defaultDrainSocket is the relay socket the chardev drainer listens on and the
// host bridge dials. [opnsense.host].upstream and [opnsense.drain].listen must
// name the same path, so both derive from this one constant to avoid drift.
const defaultDrainSocket = "/var/run/mwan-opnsense-drain.sock"

// Config is the top-level shape of the OPNsense tooling configuration.
type Config struct {
	OPNsense Section `toml:"opnsense"`
	// Source is the file Load read, so an error about a missing key names the
	// file the operator has to edit.
	Source string `toml:"-"`
}

// Section holds the [opnsense.*] subsections for the gRPC-over-virtio-serial
// transport between the Proxmox host and the OPNsense guest.
type Section struct {
	Host         HostSection     `toml:"host"`
	Drain        DrainSection    `toml:"drain"`
	Probe        ProbeSection    `toml:"probe"`
	Upgrade      UpgradeSection  `toml:"upgrade"`
	Validate     ValidateSection `toml:"validate"`
	ConfigImport ImportSection   `toml:"config_import"`
}

// ImportSection configures the `mwan opnsense config import`
// verb. Substitutions is the YAML path describing the find/replace rules
// applied to the redacted prod XML, and Output is where the transformed
// XML lands. The SOURCE argument is positional on the command line; only
// Substitutions and Output are operator-tunable enough to live in TOML.
type ImportSection struct {
	Substitutions string `toml:"substitutions"`
	Output        string `toml:"output"`
}

// HostSection configures the mwan-opnsense-host daemon that runs
// on the Proxmox host. Duration fields use the IfMgr style (string parsed
// at use site via [time.ParseDuration]) so the wire format matches the
// rest of the file.
type HostSection struct {
	Upstream                  string `toml:"upstream"`
	Listen                    string `toml:"listen"`
	ReconnectDuration         string `toml:"reconnect"`
	HeartbeatIntervalDuration string `toml:"heartbeat_interval"`
	HeartbeatTimeoutDuration  string `toml:"heartbeat_timeout"`
}

// DrainSection configures the mwan-opnsense-drain daemon that runs
// on the Proxmox host. The drainer holds the qemu virtio-serial chardev open
// and always reads it so a bridge restart never disconnects the host side and
// strands a guest write in the kernel. Chardev is the qemu chardev unix socket
// the drainer dials and holds; Listen is the relay socket the bridge dials in
// place of the chardev. See docs/ops/opnsense/wedge.md.
type DrainSection struct {
	Chardev string `toml:"chardev"`
	Listen  string `toml:"listen"`
}

// ProbeSection configures the mwan-probe client that talks to
// the host daemon over the local Unix socket.
type ProbeSection struct {
	Target           string `toml:"target"`
	TimeoutDuration  string `toml:"timeout"`
	UploadChunkBytes int    `toml:"upload_chunk_bytes"`
	// TransferStallDuration bounds file transfers by lack of progress
	// rather than total wall-clock time. A transfer succeeds as long as
	// bytes keep flowing and fails only after this much time with no
	// progress. Empty falls back to a built-in default, because a large
	// transfer must never be killed by a fixed whole-transfer deadline.
	TransferStallDuration string `toml:"transfer_stall_timeout"`
}

// UpgradeSection configures the mwan upgrade orchestrator. Operator
// tunables live here. EnvTransport is retained for forward compatibility, and
// the CLI currently uses the gRPC path.
type UpgradeSection struct {
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

	// Target is the OPNsense release the upgrade is heading toward
	// (e.g. "26.7"). It is optional. When it names a release series other
	// than the installed core package's, execute runs a major upgrade;
	// otherwise execute applies the updates pending inside the installed
	// series. Validate also receives it.
	Target string `toml:"target"`

	// DryRunExecute makes execute report the pending firmware update,
	// and whether it would reboot, without installing anything or
	// rebooting.
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
	Validate UpgradeValidateSection `toml:"validate"`
}

// UpgradeValidateSection holds the validator inputs the upgrade
// phases share with the standalone validate verb.
type UpgradeValidateSection struct {
	APIKey               string `toml:"api_key"`
	APISecret            string `toml:"api_secret"`
	BGPv4Neighbors       string `toml:"bgp_v4_neighbors"`
	BGPv6Neighbors       string `toml:"bgp_v6_neighbors"`
	OPNsenseLAN          string `toml:"opnsense_lan"`
	MWANOpnsenseSocket   string `toml:"mwan_opnsense_socket"`
	MWANOpnsenseHostSock string `toml:"mwan_opnsense_host_socket"`
	SettleAfterUpgrade   string `toml:"settle_after_upgrade"`
}

// ValidateSection configures the standalone validate verb. The
// CLI surface accepts no flags; every input lives here.
type ValidateSection struct {
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

// Load reads the file PathEnv names when it is set. Otherwise it reads
// DefaultPath, and it reads LegacyPath only when DefaultPath does not exist.
// A DefaultPath that exists but cannot be read or parsed is an error rather
// than a reason to read LegacyPath, so a broken new file is never masked.
func Load() (*Config, error) {
	override := strings.TrimSpace(os.Getenv(PathEnv))
	if override != "" {
		return loadFile(override)
	}
	return loadFirstPresent(DefaultPath, LegacyPath)
}

// loadFirstPresent reads primary, and reads legacy only when primary does not
// exist. When neither exists the error names primary, the file the deploy
// installs.
func loadFirstPresent(primary, legacy string) (*Config, error) {
	cfg, primaryErr := loadFile(primary)
	if primaryErr == nil {
		return cfg, nil
	}
	if !errors.Is(primaryErr, fs.ErrNotExist) {
		return nil, primaryErr
	}
	slog.Warn("opnsense config: file absent, reading the gateway config instead",
		"path", primary, "legacy_path", legacy)
	cfg, legacyErr := loadFile(legacy)
	if legacyErr == nil {
		return cfg, nil
	}
	if errors.Is(legacyErr, fs.ErrNotExist) {
		return nil, primaryErr
	}
	return nil, legacyErr
}

// loadFile decodes path over the defaults. Keys outside the [opnsense.*]
// tables are ignored, which is what lets it read the gateway configuration.
func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		slog.Error("opnsense config: read failed", "path", path, "err", err)
		return nil, fmt.Errorf("opnsense config: read %s: %w", path, err)
	}
	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		slog.Error("opnsense config: parse failed", "path", path, "err", err)
		return nil, fmt.Errorf("opnsense config: parse %s: %w", path, err)
	}
	cfg.Source = path
	return &cfg, nil
}

func defaultConfig() Config {
	return Config{
		OPNsense: Section{
			Host: HostSection{
				Upstream:                  "unix://" + defaultDrainSocket,
				Listen:                    "/var/run/mwan-opnsense.sock",
				ReconnectDuration:         "2s",
				HeartbeatIntervalDuration: "30s",
				HeartbeatTimeoutDuration:  "10s",
			},
			Drain: DrainSection{
				Chardev: "unix:///var/run/qemu-server/101.mwanrpc",
				Listen:  defaultDrainSocket,
			},
			Probe: ProbeSection{
				Target:                "unix:///var/run/mwan-opnsense.sock",
				TimeoutDuration:       "10s",
				UploadChunkBytes:      16384,
				TransferStallDuration: "30s",
			},
			Upgrade: UpgradeSection{
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
				Validate: UpgradeValidateSection{
					APIKey:               "",
					APISecret:            "",
					BGPv4Neighbors:       "",
					BGPv6Neighbors:       "",
					OPNsenseLAN:          "",
					MWANOpnsenseSocket:   "",
					MWANOpnsenseHostSock: "",
					SettleAfterUpgrade:   "5m",
				},
			},
			Validate: ValidateSection{
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
			},
			ConfigImport: ImportSection{
				Substitutions: "",
				Output:        "",
			},
		},
		Source: "",
	}
}
