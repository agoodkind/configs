package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

const (
	// BaselineSchemaVersion is the schema version embedded in
	// every persisted baseline JSON. The value travels with the
	// file so a reader can refuse to consume a stale shape.
	BaselineSchemaVersion = 1

	// DefaultStateDir is the on-disk root for upgrade artefacts.
	// The path matches MWAN-152's state directory per resolved
	// decision O-6 in the matrix doc (singular "upgrade", not
	// "upgrades").
	DefaultStateDir = "/var/lib/mwan/upgrade"

	// PreBaselineFilename is the file name written under
	// <state_dir>/<vmid>/<deploy_id>/.
	PreBaselineFilename = "pre-baseline.json"

	// PostResultFilename is the post-upgrade matrix run.
	PostResultFilename = "post-result.json"

	// DiffReportFilename is the per-check diff outcome record.
	DiffReportFilename = "diff-report.json"
)

// Baseline is the persisted output of a baseline-capture run. The
// shape doubles as a "post-result" record, since the diff step needs
// two records of the same type.
type Baseline struct {
	SchemaVersion   int       `json:"schema_version"`
	CapturedAt      time.Time `json:"captured_at"`
	VMID            int       `json:"vmid"`
	DeployID        string    `json:"deploy_id,omitempty"`
	OPNsenseVersion string    `json:"opnsense_version,omitempty"`
	ProxmoxNode     string    `json:"proxmox_node,omitempty"`

	// Plugins is the list of os-* plugins discovered on the host.
	// Per resolved decision O-2 the runner reads this from the
	// baseline rather than hard-coding it. A nil slice during a
	// post-upgrade run means "fall back to the baseline".
	Plugins []string `json:"plugins,omitempty"`

	// CaptivePortalZones is the list of zone UUIDs found in the
	// configuration. An empty slice means the captive portal core
	// feature is not enabled and the matching checks are skipped.
	CaptivePortalZones []string `json:"captive_portal_zones,omitempty"`

	// VtnetIndices lists the vtnet driver indices present on the
	// host (for example {0, 1, 2}). Used by the kernel/driver
	// checks introduced in resolved decision O-5.
	VtnetIndices []int `json:"vtnet_indices,omitempty"`

	// InterfacesSet is the set of interfaces the post-upgrade
	// run is expected to still see. Populated by
	// interfaces_set_unchanged at baseline time.
	InterfacesSet []string `json:"interfaces_set,omitempty"`

	// DHCPv4LeaseCount is the lease count at baseline time.
	// Tolerance is computed as max(2, 0.1 * count) per resolved
	// decision O-1.
	DHCPv4LeaseCount int `json:"dhcpv4_lease_count,omitempty"`

	// DHCPv6IANALeaseCount is the IA-NA lease count at baseline.
	DHCPv6IANALeaseCount int `json:"dhcpv6_ia_na_lease_count,omitempty"`

	// DHCPv6IAPDLeaseCount is the IA-PD lease count at baseline.
	DHCPv6IAPDLeaseCount int `json:"dhcpv6_ia_pd_lease_count,omitempty"`

	// PFRuleCount is the pf rule count at baseline.
	PFRuleCount int `json:"pf_rule_count,omitempty"`

	// PFNatRuleCount is the pf NAT rule count at baseline.
	PFNatRuleCount int `json:"pf_nat_rule_count,omitempty"`

	// Results is the per-check raw record list.
	Results []Result `json:"results"`
}

// ResultByID is a convenience lookup the diff step uses heavily.
func (b *Baseline) ResultByID(id string) (Result, bool) {
	if b == nil {
		return emptyResult(), false
	}
	for _, r := range b.Results {
		if r.CheckID == id {
			return r, true
		}
	}
	return emptyResult(), false
}

// HasPlugin reports whether the given os-* plugin appears in the
// baseline plugin set. Used by AppliesWhen predicates so a host
// without os-tayga skips the os-tayga checks.
func (b *Baseline) HasPlugin(name string) bool {
	if b == nil {
		return true
	}
	return slices.Contains(b.Plugins, name)
}

// ArtefactPath returns the canonical artefact directory for a given
// vmid and deploy id, rooted at stateDir. The directory is created
// on first write, not on construction.
func ArtefactPath(stateDir string, vmid int, deployID string) string {
	return filepath.Join(stateDir, strconv.Itoa(vmid), deployID)
}

// SaveBaseline persists b to <stateDir>/<vmid>/<deployID>/<name>,
// creating parent directories as needed. The file is written via a
// temp file and atomic rename so a reader never sees a partial
// write.
func SaveBaseline(stateDir string, vmid int, deployID, name string, b *Baseline) error {
	if b == nil {
		return errors.New("validate: cannot save nil baseline")
	}
	if name == "" {
		return errors.New("validate: baseline filename required")
	}
	dir := ArtefactPath(stateDir, vmid, deployID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("validate SaveBaseline mkdir failed",
			"dir", dir, "err", err.Error())
		return fmt.Errorf("create artefact dir: %w", err)
	}
	final := filepath.Join(dir, name)
	tmp := final + ".tmp"
	encoded, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		slog.Error("validate SaveBaseline encode failed",
			"err", err.Error())
		return fmt.Errorf("encode baseline: %w", err)
	}
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		slog.Error("validate SaveBaseline write tmp failed",
			"path", tmp, "err", err.Error())
		return fmt.Errorf("write baseline tmp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		slog.Error("validate SaveBaseline rename failed",
			"from", tmp, "to", final, "err", err.Error())
		return fmt.Errorf("rename baseline: %w", err)
	}
	slog.Info("validate baseline saved",
		"state_dir", stateDir, "vmid", vmid,
		"deploy_id", deployID, "filename", name)
	return nil
}

// LoadBaseline reads a baseline JSON file from path and validates
// the schema version. A schema version other than the current one
// is rejected; the caller should regenerate.
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Error("validate LoadBaseline read failed",
			"path", path, "err", err.Error())
		return nil, fmt.Errorf("read baseline %s: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		slog.Error("validate LoadBaseline decode failed",
			"path", path, "err", err.Error())
		return nil, fmt.Errorf("decode baseline %s: %w", path, err)
	}
	if b.SchemaVersion != BaselineSchemaVersion {
		err := fmt.Errorf(
			"baseline %s: schema_version %d unsupported (want %d)",
			path, b.SchemaVersion, BaselineSchemaVersion)
		slog.Error("validate LoadBaseline schema mismatch",
			"path", path,
			"got", b.SchemaVersion,
			"want", BaselineSchemaVersion,
			"err", err.Error())
		return nil, err
	}
	return &b, nil
}
