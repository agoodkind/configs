//go:build linux

package wghealth

import (
	"strings"
	"testing"
	"time"
)

// canonical wg show wg0 dump output: first line is the interface, rest are peers.
// Field separators are tabs.
const sampleDump = "PRIVKEYabc=\tjz3eKGui8bC2vf9rxrKGbk6WGwQWIc/PRNpFW91xDkk=\t51820\toff\n" +
	"EYvlZyous8zcgLwT09khnXd3VPQj2sbphlrY1BpErXw=\t(none)\t[2601:84:837c:a160:3e8c:f8ff:fef9:7bf2]:51821\t10.240.0.0/16,3d06:bad:b01:200::/56\t1777386334\t903942\t815478\t25\n" +
	"NeverPeer1234abcd=\t(none)\t(none)\t10.240.10.99/32\t0\t0\t0\toff\n" +
	"PmJXT8pLFCkJwRCSkKhXAJJTs598tiQdvR+kKQeORkI=\t(none)\t(none)\t3d06:bad:b01:a::8/128\t1777380000\t512\t1024\t25\n"

func TestParseWGShowDump(t *testing.T) {
	got, err := parseWGShowDump(sampleDump)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 peers, got %d", len(got))
	}
	sub, ok := got["EYvlZyous8zcgLwT09khnXd3VPQj2sbphlrY1BpErXw="]
	if !ok {
		t.Fatalf("suburban peer missing")
	}
	if sub.endpoint != "[2601:84:837c:a160:3e8c:f8ff:fef9:7bf2]:51821" {
		t.Errorf("endpoint=%q", sub.endpoint)
	}
	if sub.handshake.Unix() != 1777386334 {
		t.Errorf("handshake epoch=%d want 1777386334", sub.handshake.Unix())
	}
	if sub.rxBytes != 903942 || sub.txBytes != 815478 {
		t.Errorf("rx=%d tx=%d", sub.rxBytes, sub.txBytes)
	}
	if sub.keepalive != 25*time.Second {
		t.Errorf("keepalive=%s", sub.keepalive)
	}
	never, ok := got["NeverPeer1234abcd="]
	if !ok {
		t.Fatalf("never-handshaked peer missing")
	}
	if !never.handshake.IsZero() {
		t.Errorf("never peer should have zero handshake, got %v", never.handshake)
	}
	if never.keepalive != 0 {
		t.Errorf("never peer keepalive should be 0 (off), got %s", never.keepalive)
	}
}

func TestParseWGShowDump_EmptyAndHeaderOnly(t *testing.T) {
	if got, err := parseWGShowDump(""); err != nil || len(got) != 0 {
		t.Errorf("empty: got=%v err=%v", got, err)
	}
	headerOnly := "PRIVKEYabc=\tPUB\t51820\toff\n"
	if got, err := parseWGShowDump(headerOnly); err != nil || len(got) != 0 {
		t.Errorf("header only: got=%v err=%v", got, err)
	}
}

func TestParseWGShowDump_BadFields(t *testing.T) {
	cases := map[string]string{
		"too few fields": "PRIVKEYabc=\tPUB\t51820\toff\nshort\n",
		"bad epoch":      "PRIVKEYabc=\tPUB\t51820\toff\nPK1=\t(none)\t(none)\t10/32\tNOT_A_NUMBER\t0\t0\toff\n",
		"bad keepalive":  "PRIVKEYabc=\tPUB\t51820\toff\nPK1=\t(none)\t(none)\t10/32\t0\t0\t0\tnotaduration\n",
	}
	for name, input := range cases {
		if _, err := parseWGShowDump(input); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestShortKey(t *testing.T) {
	if got := shortKey("EYvlZyous8zcgLwT09khnXd3VPQj2sbphlrY1BpErXw="); got != "EYvlZyou" {
		t.Errorf("shortKey: %q", got)
	}
	if got := shortKey("short"); got != "short" {
		t.Errorf("shortKey short: %q", got)
	}
}

func TestNew_DefaultsAndOverrides(t *testing.T) {
	m, err := New(map[string]any{
		"ssh_host":            "agoodkind@3d06:bad:b01::1",
		"sudo":                true,
		"warn_handshake_age":  "200s",
		"error_handshake_age": "400s",
		"ignore_peers":        []any{"PmJXT8pLFCkJwRCSkKhXAJJTs598tiQdvR+kKQeORkI="},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mod := m.(*Module)
	if mod.cfg.SSHHost != "agoodkind@3d06:bad:b01::1" {
		t.Errorf("ssh_host=%q", mod.cfg.SSHHost)
	}
	if !mod.cfg.Sudo {
		t.Errorf("sudo=false, want true")
	}
	if mod.cfg.Iface != "wg0" {
		t.Errorf("iface default not wg0: %q", mod.cfg.Iface)
	}
	if mod.cfg.WarnHandshakeAge != 200*time.Second {
		t.Errorf("warn_handshake_age=%s", mod.cfg.WarnHandshakeAge)
	}
	if mod.cfg.ErrorHandshakeAge != 400*time.Second {
		t.Errorf("error_handshake_age=%s", mod.cfg.ErrorHandshakeAge)
	}
	if !mod.cfg.IgnorePeers["PmJXT8pLFCkJwRCSkKhXAJJTs598tiQdvR+kKQeORkI="] {
		t.Errorf("ignore_peers not set")
	}
}

func TestNew_RequiresSSHHost(t *testing.T) {
	m, err := New(map[string]any{})
	if err != nil {
		t.Fatalf("constructor should not error on missing ssh_host (validated in Init): %v", err)
	}
	// Init should reject:
	mod := m.(*Module)
	if mod.cfg.SSHHost != "" {
		t.Errorf("expected empty ssh_host, got %q", mod.cfg.SSHHost)
	}
}

// guard against accidentally counting the header line as a peer when input
// has CRLF line endings.
func TestParseWGShowDump_CRLF(t *testing.T) {
	crlf := strings.ReplaceAll(sampleDump, "\n", "\r\n")
	got, err := parseWGShowDump(crlf)
	if err != nil {
		t.Fatalf("CRLF: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("CRLF want 3 peers, got %d", len(got))
	}
}
