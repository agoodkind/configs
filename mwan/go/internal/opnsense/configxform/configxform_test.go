package configxform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func loadFixture(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("testdata", "minimal-config.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	return data
}

func mustParse(t *testing.T, data []byte) *etree.Document {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		t.Fatalf("parse XML: %v", err)
	}
	return doc
}

func defaultSubs() Substitutions {
	return Substitutions{
		DeviceNames: []DeviceNameMapping{
			{Name: "iavf0 to vtnet0", From: "iavf0", To: "vtnet0"},
		},
		XPathSets: []XPathSet{
			{Name: "hostname", XPath: "//opnsense/system/hostname", NewValue: "router-test"},
			{Name: "domain", XPath: "//opnsense/system/domain", NewValue: "test.home.goodkind.io"},
			{Name: "wan ipaddr", XPath: "//opnsense/interfaces/wan/ipaddr", NewValue: "10.240.250.2"},
			{Name: "wan ipaddrv6", XPath: "//opnsense/interfaces/wan/ipaddrv6", NewValue: "3d06:bad:b01:2fe::2"},
			{Name: "mgmt v4", XPath: "//opnsense/interfaces/opt9/ipaddr", NewValue: "10.240.4.1"},
			{Name: "mgmt v6", XPath: "//opnsense/interfaces/opt9/ipaddrv6", NewValue: "3d06:bad:b01:204::1"},
		},
		RemoveElements: []ElementRemove{
			{Name: "wireguard peers", XPath: "//opnsense/OPNsense/wireguard/client/clients/client"},
		},
		TextLiterals: []TextLiteral{
			{Name: "nat source net", From: "10.250.0.0/24", To: "10.240.0.0/24"},
		},
	}
}

func TestApplyRoundTripDeviceNames(t *testing.T) {
	input := loadFixture(t)
	out, err := Apply(input, defaultSubs())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc := mustParse(t, out)
	ifs := doc.FindElements("//opnsense//if")
	if len(ifs) == 0 {
		t.Fatal("expected at least one <if> element after transform")
	}
	for _, el := range ifs {
		got := strings.TrimSpace(el.Text())
		if got == "iavf0" {
			t.Errorf("found stale iavf0 reference at %s", el.GetPath())
		}
		if got != "vtnet0" {
			t.Errorf("unexpected <if> value %q at %s; want vtnet0", got, el.GetPath())
		}
	}
}

func TestApplyXPathHostnameAndDomain(t *testing.T) {
	out, err := Apply(loadFixture(t), defaultSubs())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc := mustParse(t, out)
	cases := []struct {
		xpath string
		want  string
	}{
		{"//opnsense/system/hostname", "router-test"},
		{"//opnsense/system/domain", "test.home.goodkind.io"},
		{"//opnsense/interfaces/wan/ipaddr", "10.240.250.2"},
		{"//opnsense/interfaces/wan/ipaddrv6", "3d06:bad:b01:2fe::2"},
		{"//opnsense/interfaces/opt9/ipaddr", "10.240.4.1"},
		{"//opnsense/interfaces/opt9/ipaddrv6", "3d06:bad:b01:204::1"},
	}
	for _, tc := range cases {
		t.Run(tc.xpath, func(t *testing.T) {
			el := doc.FindElement(tc.xpath)
			if el == nil {
				t.Fatalf("xpath %q matched no element", tc.xpath)
			}
			got := strings.TrimSpace(el.Text())
			if got != tc.want {
				t.Errorf("xpath %q: got %q, want %q", tc.xpath, got, tc.want)
			}
		})
	}
}

func TestApplyTextLiteralReplace(t *testing.T) {
	out, err := Apply(loadFixture(t), defaultSubs())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if strings.Contains(string(out), "10.250.0.0/24") {
		t.Errorf("output still contains prod literal 10.250.0.0/24")
	}
	if !strings.Contains(string(out), "10.240.0.0/24") {
		t.Errorf("output missing testbed literal 10.240.0.0/24")
	}
}

func TestApplyRemovesWireguardPeers(t *testing.T) {
	out, err := Apply(loadFixture(t), defaultSubs())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc := mustParse(t, out)
	peers := doc.FindElements("//opnsense/OPNsense/wireguard/client/clients/client")
	if len(peers) != 0 {
		t.Errorf("expected zero wireguard peers after strip, got %d", len(peers))
	}
}

func TestApplyEmptyInputReturnsError(t *testing.T) {
	if _, err := Apply(nil, defaultSubs()); err == nil {
		t.Fatal("Apply(nil) returned no error")
	}
}

func TestApplyMalformedXMLReturnsError(t *testing.T) {
	bad := []byte("<opnsense><unclosed>")
	if _, err := Apply(bad, defaultSubs()); err == nil {
		t.Fatal("Apply on malformed XML returned no error")
	}
}

func TestDecodeMalformedYAMLReturnsError(t *testing.T) {
	bad := []byte("device_names: [unterminated\n")
	if _, err := Decode(bad); err == nil {
		t.Fatal("Decode on malformed YAML returned no error")
	}
}

func TestDecodeUnknownFieldReturnsError(t *testing.T) {
	bad := []byte("not_a_known_field: 1\n")
	if _, err := Decode(bad); err == nil {
		t.Fatal("Decode on unknown field returned no error")
	}
}

func TestDecodeEmptyInputReturnsError(t *testing.T) {
	if _, err := Decode(nil); err == nil {
		t.Fatal("Decode(nil) returned no error")
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	yamlBytes := []byte(`device_names:
  - name: "iavf0 to vtnet0"
    from: "iavf0"
    to:   "vtnet0"
xpath_sets:
  - name: "hostname"
    xpath: "//opnsense/system/hostname"
    new_value: "router-test"
remove_elements:
  - name: "wg peers"
    xpath: "//opnsense/OPNsense/wireguard/client/clients/client"
text_literals:
  - name: "nat src"
    from: "10.250.0.0/24"
    to:   "10.240.0.0/24"
`)
	got, err := Decode(yamlBytes)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got.DeviceNames) != 1 || got.DeviceNames[0].From != "iavf0" || got.DeviceNames[0].To != "vtnet0" {
		t.Errorf("DeviceNames roundtrip mismatch: %+v", got.DeviceNames)
	}
	if len(got.XPathSets) != 1 || got.XPathSets[0].NewValue != "router-test" {
		t.Errorf("XPathSets roundtrip mismatch: %+v", got.XPathSets)
	}
	if len(got.RemoveElements) != 1 {
		t.Errorf("RemoveElements roundtrip mismatch: %+v", got.RemoveElements)
	}
	if len(got.TextLiterals) != 1 || got.TextLiterals[0].To != "10.240.0.0/24" {
		t.Errorf("TextLiterals roundtrip mismatch: %+v", got.TextLiterals)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join("testdata", "does-not-exist.yaml")); err == nil {
		t.Fatal("Load on missing file returned no error")
	}
}

func TestApplyDeviceNameRejectsEmptyMapping(t *testing.T) {
	subs := Substitutions{
		DeviceNames: []DeviceNameMapping{{Name: "broken", From: "", To: "vtnet0"}},
	}
	if _, err := Apply(loadFixture(t), subs); err == nil {
		t.Fatal("Apply with empty From mapping returned no error")
	}
}

func TestApplyXPathSetRejectsEmptyXPath(t *testing.T) {
	subs := Substitutions{
		XPathSets: []XPathSet{{Name: "broken", XPath: "", NewValue: "x"}},
	}
	if _, err := Apply(loadFixture(t), subs); err == nil {
		t.Fatal("Apply with empty XPath returned no error")
	}
}

func TestApplyXPathSetMissingMatchIsTolerated(t *testing.T) {
	subs := Substitutions{
		XPathSets: []XPathSet{
			{Name: "no match", XPath: "//opnsense/system/this-element-does-not-exist", NewValue: "value"},
		},
	}
	if _, err := Apply(loadFixture(t), subs); err != nil {
		t.Fatalf("Apply with no-match XPath should not error, got: %v", err)
	}
}
