//go:build linux

package networkjson_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goodkind.io/mwan/internal/networkjson"
)

// schemaDirForTest assembles the model set the gateway installs into a
// temporary directory: the vendored IETF modules at the revisions the deploy
// copies, plus the repository's steering module at whatever revision it
// currently carries, so a revision bump does not touch this test.
func schemaDirForTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sources := []string{
		"../../../../third_party/yang/standard/ietf/RFC/ietf-yang-types@2025-12-22.yang",
		"../../../../third_party/yang/standard/ietf/RFC/ietf-inet-types@2025-12-22.yang",
		"../../../../third_party/yang/standard/ietf/RFC/iana-if-type@2014-05-08.yang",
		"../../../../third_party/yang/standard/ietf/RFC/ietf-interfaces@2018-02-20.yang",
		"../../../../third_party/yang/standard/ietf/RFC/ietf-ip@2018-02-22.yang",
		"../../../../third_party/yang/standard/ietf/RFC/ietf-nat@2019-01-10.yang",
	}
	matches, err := filepath.Glob("../../../yang/goodkind-mwan-steering@*.yang")
	if err != nil {
		t.Fatalf("glob steering model: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly one steering model, found %d", len(matches))
	}
	for _, source := range append(sources, matches[0]) {
		content, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		target := filepath.Join(dir, filepath.Base(source))
		if err := os.WriteFile(target, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
	}
	return dir
}

// validDocument is one gateway's network tree: two providers, one of them with
// an IPv4 source pin and one with no probe at all, plus the internal link and
// the group-wide values. Addresses are documentation prefixes.
const validDocument = `{
  "ietf-interfaces:interfaces": {
    "interface": [
      {
        "name": "enwebpass0",
        "type": "iana-if-type:other",
        "goodkind-mwan-steering:wan": {
          "name": "webpass",
          "table-id": 200,
          "fw-mark": 2,
          "fw-mark-prio": 200,
          "from-prio": 56,
          "npt-prefix": "2001:db8:beef:200::/60",
          "v4-source": "203.0.113.2",
          "health": {
            "enabled": true,
            "ping-count": 3,
            "success-threshold": 2,
            "failure-threshold": 2,
            "recovery-threshold": 2,
            "check-interval": 10,
            "targets-v4": ["192.0.2.10", "192.0.2.11"],
            "targets-v6": ["2001:db8:53::1", "2001:db8:53::2"],
            "http-urls": ["https://example.test/ip"]
          }
        }
      },
      {
        "name": "enatt0",
        "type": "iana-if-type:other",
        "goodkind-mwan-steering:wan": {
          "name": "att",
          "table-id": 100,
          "fw-mark": 1,
          "fw-mark-prio": 100,
          "from-prio": 55,
          "npt-prefix": "2001:db8:beef:100::/60"
        }
      },
      { "name": "enmwanbr0", "type": "iana-if-type:other" }
    ],
    "goodkind-mwan-steering:steering-group": {
      "translation": {
        "internal-prefix": "2001:db8:b01::/60",
        "opnsense-edge-v6": "2001:db8:b01:fe::2",
        "mwanbr-edge-v6": "2001:db8:b01:fe::3"
      },
      "routes": {
        "internal-iface": "enmwanbr0",
        "internal-net-v4": "192.0.2.0/29"
      },
      "health": { "probe-timeout": 2000 }
    }
  }
}`

// writeDocument puts body in a file the loader can read.
func writeDocument(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "network.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}
	return path
}

func TestLoadValidFile(t *testing.T) {
	t.Parallel()

	loaded, err := networkjson.Load(writeDocument(t, validDocument), schemaDirForTest(t))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := len(loaded.WAN); got != 2 {
		t.Fatalf("provider count = %d, want 2", got)
	}
	if got := loaded.WAN["webpass"].Iface; got != "enwebpass0" {
		t.Fatalf("webpass iface = %q, want enwebpass0", got)
	}
	if got := loaded.WAN["webpass"].TableID; got != 200 {
		t.Fatalf("webpass table id = %d, want 200", got)
	}
	if got := loaded.WAN["webpass"].V4Source; got != "203.0.113.2" {
		t.Fatalf("webpass v4 source = %q, want 203.0.113.2", got)
	}
	if got := loaded.WAN["att"].V4Source; got != "" {
		t.Fatalf("att v4 source = %q, want empty", got)
	}
	if _, probed := loaded.Health["att"]; probed {
		t.Fatal("att carries no health container and must hold no probe")
	}
	if got := loaded.Health["webpass"].CheckIntervalSeconds; got != 10 {
		t.Fatalf("webpass check interval = %d, want 10", got)
	}
	if got := loaded.Health["webpass"].SuccessThreshold; got != 2 {
		t.Fatalf("webpass success threshold = %d, want 2", got)
	}
	if got := loaded.ProbeTimeoutMillis; got != 2000 {
		t.Fatalf("probe timeout = %d, want 2000", got)
	}
	if got := loaded.InternalPrefix; got != "2001:db8:b01::/60" {
		t.Fatalf("internal prefix = %q, want 2001:db8:b01::/60", got)
	}
	if got := loaded.InternalIface; got != "enmwanbr0" {
		t.Fatalf("internal iface = %q, want enmwanbr0", got)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := networkjson.Load(writeDocument(t, "{not json"), schemaDirForTest(t))
	if err == nil {
		t.Fatal("Load accepted a file that is not JSON")
	}
}

func TestLoadRejectsSchemaViolation(t *testing.T) {
	t.Parallel()

	// The schema bounds fw-mark at 1 or higher, which is the check the daemon
	// makes on every provider today.
	body := strings.Replace(validDocument, `"fw-mark": 2,`, `"fw-mark": 0,`, 1)
	_, err := networkjson.Load(writeDocument(t, body), schemaDirForTest(t))
	if err == nil {
		t.Fatal("Load accepted a zero firewall mark")
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := networkjson.Load(filepath.Join(t.TempDir(), "absent.json"), schemaDirForTest(t))
	if err == nil {
		t.Fatal("Load accepted a missing file")
	}
}

func TestLoadRejectsMissingRequiredLeaf(t *testing.T) {
	t.Parallel()

	// The schema leaves table-id optional, because a leaf's type cannot see
	// whether the daemon needs it. The loader is where that requirement lives,
	// so an absent table id must fail rather than default to zero.
	body := strings.Replace(validDocument, `"table-id": 100,`, ``, 1)
	_, err := networkjson.Load(writeDocument(t, body), schemaDirForTest(t))
	if err == nil {
		t.Fatal("Load accepted a provider with no table id")
	}
	if !strings.Contains(err.Error(), "table-id") {
		t.Fatalf("error does not name the missing leaf: %v", err)
	}
}
