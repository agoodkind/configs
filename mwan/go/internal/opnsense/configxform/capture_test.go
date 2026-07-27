package configxform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoPath resolves a path relative to the repository root. The package lives
// at mwan/go/internal/opnsense/configxform, so the root is five levels up.
func repoPath(parts ...string) string {
	base := filepath.Join("..", "..", "..", "..", "..")
	return filepath.Join(append([]string{base}, parts...)...)
}

// TestApplyRealCaptureWithCommittedSubstitutions runs the committed
// substitution table against the committed prod capture. Every other test in
// this package uses a small hand-written fixture, so nothing else notices when
// the capture and the substitutions drift apart. A refreshed capture whose
// shape no longer matches the table fails here rather than at import time on
// the testbed router.
func TestApplyRealCaptureWithCommittedSubstitutions(t *testing.T) {
	capturePath := repoPath("testbed", "opnsense", "prod-config-redacted.xml")
	input, err := os.ReadFile(capturePath) //nolint:gosec // fixed repo-relative path
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
	doc := mustParse(t, out)

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
		"//opnsense/interfaces/opt6",
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
