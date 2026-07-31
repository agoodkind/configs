package configxform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoPath resolves a path relative to the repository root. The package lives
// at mwan/go/internal/opnsense/configxform, so the root is five levels up.
func repoPath(parts ...string) string {
	base := filepath.Join("..", "..", "..", "..", "..")
	return filepath.Join(append([]string{base}, parts...)...)
}

// readRepoFile reads a committed repository file. Every caller passes a path
// built by repoPath from string literals, so the path is fixed at compile time
// even though it is not a constant expression.
func readRepoFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(path)) //nolint:gosec // fixed repo-relative path
}

// transformRealCapture applies the committed substitution table to the
// committed prod capture and returns the candidate bytes.
func transformRealCapture(t *testing.T) []byte {
	t.Helper()
	capturePath := repoPath("testbed", "opnsense", "prod-config-redacted.xml")
	input, err := readRepoFile(capturePath)
	if err != nil {
		t.Fatalf("read capture %q: %v", capturePath, err)
	}

	subsPath := repoPath("testbed", "opnsense", "substitutions.yaml")
	subs, err := Load(subsPath)
	if err != nil {
		t.Fatalf("load substitutions %q: %v", subsPath, err)
	}

	out, err := Apply(input, subs)
	if err != nil {
		t.Fatalf("Apply on real capture: %v", err)
	}
	return out
}

// serviceMappingEntry is the subset of a service_mapping.yml guest entry that
// the substitution table has to agree with.
type serviceMappingEntry struct {
	IPv4Transit string `yaml:"ipv4_transit"`
	IPv6Transit string `yaml:"ipv6_transit"`
}

// TestSubstitutionsMatchServiceMapping keeps the substitution table honest
// against the addressing source of truth. The table is standalone input to
// `mwan opnsense config import`, so nothing renders it from service_mapping.
// Without this, a renumber there leaves the transform writing the old address
// and the divergence only surfaces on the router after an import.
func TestSubstitutionsMatchServiceMapping(t *testing.T) {
	mappingPath := repoPath("ansible", "inventory", "group_vars", "all", "service_mapping.yml")
	mappingBytes, err := readRepoFile(mappingPath)
	if err != nil {
		t.Fatalf("read service mapping %q: %v", mappingPath, err)
	}
	var mapping struct {
		ServiceMapping map[string]serviceMappingEntry `yaml:"service_mapping"`
	}
	if err := yaml.Unmarshal(mappingBytes, &mapping); err != nil {
		t.Fatalf("parse service mapping: %v", err)
	}
	router, ok := mapping.ServiceMapping["opnsense_test"]
	if !ok {
		t.Fatal("service_mapping has no opnsense_test entry")
	}

	subs, err := Load(repoPath("testbed", "opnsense", "substitutions.yaml"))
	if err != nil {
		t.Fatalf("load substitutions: %v", err)
	}
	byXPath := make(map[string]string, len(subs.XPathSets))
	for _, set := range subs.XPathSets {
		byXPath[set.XPath] = set.NewValue
	}

	for _, tc := range []struct{ xpath, want string }{
		{"//opnsense/interfaces/wan/ipaddr", router.IPv4Transit},
		{"//opnsense/interfaces/wan/ipaddrv6", router.IPv6Transit},
	} {
		got, present := byXPath[tc.xpath]
		if !present {
			t.Errorf("substitutions set no value for %s", tc.xpath)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %q, but service_mapping says %q", tc.xpath, got, tc.want)
		}
	}
}

// TestApplyRealCaptureWithCommittedSubstitutions runs the committed
// substitution table against the committed prod capture. Every other test in
// this package uses a small hand-written fixture, so nothing else notices when
// the capture and the substitutions drift apart. A refreshed capture whose
// shape no longer matches the table fails here rather than at import time on
// the testbed router.
func TestApplyRealCaptureWithCommittedSubstitutions(t *testing.T) {
	doc := mustParse(t, transformRealCapture(t))

	if el := doc.FindElement("//opnsense/system/hostname"); el == nil {
		t.Error("transformed capture has no hostname element")
	} else if got := strings.TrimSpace(el.Text()); got != "router-test" {
		t.Errorf("hostname = %q, want router-test", got)
	}

	for _, el := range doc.FindElements("//opnsense//if") {
		if strings.TrimSpace(el.Text()) == "iavf0" {
			t.Errorf("stale prod device iavf0 survives at %s", el.GetPath())
		}
	}

	for _, xpath := range []string{
		"//opnsense/interfaces/opt9",
		"//opnsense/cert",
		"//opnsense/OPNsense/wireguard/client/clients/client",
		"//opnsense/OPNsense/wireguard/server/servers/server",
	} {
		if n := len(doc.FindElements(xpath)); n != 0 {
			t.Errorf("%s: %d elements survive, want 0", xpath, n)
		}
	}

	// The capture must carry the post-26.7 shape. Router advertisements moved
	// out of the per-interface dhcpdv6 model into OPNsense/radvd, so a ramode
	// element means the capture predates the migration the live router ran.
	if n := len(doc.FindElements("//ramode")); n != 0 {
		t.Errorf("capture carries %d pre-26.7 ramode elements", n)
	}
}

// TestApplyRealCapturePlacesGuestsOnVMNET checks the renumber end to end on the
// real capture: the guest segment lands on VMNET inside the translated /60, the
// MANAGEMENT segment is gone along with everything that referenced it, and the
// transit, IPv6-only, and NAT64 addresses are left alone.
func TestApplyRealCapturePlacesGuestsOnVMNET(t *testing.T) {
	out := transformRealCapture(t)
	doc := mustParse(t, out)

	for _, tc := range []struct{ xpath, want string }{
		{"//opnsense/interfaces/opt6/if", "vtnet0"},
		{"//opnsense/interfaces/opt6/ipaddr", "10.240.4.1"},
		{"//opnsense/interfaces/opt6/ipaddrv6", "3d06:bad:b01:210::1"},
		{"//opnsense/interfaces/lan/ipaddrv6", "3d06:bad:b01:211::1"},
		{"//opnsense/interfaces/opt4/ipaddrv6", "3d06:bad:b01:212::1"},
		{"//opnsense/interfaces/wan/ipaddr", "10.240.240.2"},
		{"//opnsense/interfaces/wan/ipaddrv6", "3d06:bad:b01:201::2"},
		{"//opnsense/interfaces/opt8/ipaddrv6", "3d06:bad:b01:264::1"},
		{"//opnsense/OPNsense/tayga/general/v6prefix", "3d06:bad:b01:2664::/96"},
	} {
		el := doc.FindElement(tc.xpath)
		if el == nil {
			t.Errorf("%s matched no element", tc.xpath)
			continue
		}
		if got := strings.TrimSpace(el.Text()); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.xpath, got, tc.want)
		}
	}

	// Every VLAN must hang off the device the testbed VM actually has.
	for _, el := range doc.FindElements("//opnsense/vlans/vlan/if") {
		if got := strings.TrimSpace(el.Text()); got != "vtnet0" {
			t.Errorf("VLAN parent = %q, want vtnet0", got)
		}
	}

	// Removing MANAGEMENT has to take its references with it, or the candidate
	// carries rules, a virtual IP, and a group member pointing at an interface
	// that no longer exists.
	if strings.Contains(string(out), "opt9") {
		t.Error("candidate still references the removed MANAGEMENT interface")
	}

	// Any surviving prod literal means a shift was missed.
	for _, stale := range []string{
		"3d06:bad:b01:21::", "3d06:bad:b01:22::", "3d06:bad:b01:23::",
		"3d06:bad:b01:204::", "3d06:bad:b01:200::",
		"10.250.250.",
		"10.250.0.", "10.250.1.", "10.250.2.", "10.250.3.", "10.250.4.",
	} {
		if n := strings.Count(string(out), stale); n != 0 {
			t.Errorf("stale literal %q appears %d times in the candidate", stale, n)
		}
	}
}
