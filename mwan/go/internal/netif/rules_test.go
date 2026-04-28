//go:build linux

package netif

import (
	"net"
	"testing"
)

// Note: TestParseRuleList was removed when rules.go switched to
// vishvananda/netlink. The string parser it covered no longer exists.
// Equivalent coverage is now via integration tests on a real Linux host
// (testbed LXC 100, prod vault) where RuleList round-trips through the
// kernel.

func TestRulesMatch(t *testing.T) {
	cur := CurrentRule{Priority: 5, UIDRange: "997-997", TableID: 500}
	cases := []struct {
		name string
		want DesiredRule
		ok   bool
	}{
		{"exact", DesiredRule{Priority: 5, UIDRange: "997-997", TableID: 500}, true},
		{"diff prio", DesiredRule{Priority: 6, UIDRange: "997-997", TableID: 500}, false},
		{"diff uid", DesiredRule{Priority: 5, UIDRange: "996-996", TableID: 500}, false},
		{"diff table", DesiredRule{Priority: 5, UIDRange: "997-997", TableID: 254}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rulesMatch(cur, tc.want) != tc.ok {
				t.Fatalf("rulesMatch(%+v, %+v) = %v, want %v",
					cur, tc.want, !tc.ok, tc.ok)
			}
		})
	}
}

func TestParseUIDRange(t *testing.T) {
	cases := []struct {
		in      string
		wantLo  int
		wantHi  int
		wantErr bool
	}{
		{"997-997", 997, 997, false},
		{"100-200", 100, 200, false},
		{"42", 42, 42, false},
		{"abc", 0, 0, true},
		{"100-x", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			lo, hi, err := parseUIDRange(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && (lo != tc.wantLo || hi != tc.wantHi) {
				t.Fatalf("got %d-%d, want %d-%d", lo, hi, tc.wantLo, tc.wantHi)
			}
		})
	}
}

func TestParseSelectorIPv6Single(t *testing.T) {
	// RFC 3849 documentation prefix
	ipnet, err := parseSelector("inet6", "2001:db8::1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ones, _ := ipnet.Mask.Size(); ones != 128 {
		t.Fatalf("expected /128 mask, got /%d", ones)
	}
}

func TestParseSelectorIPv4CIDR(t *testing.T) {
	// RFC 5737 documentation prefix
	ipnet, err := parseSelector("inet", "192.0.2.0/24")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ones, _ := ipnet.Mask.Size(); ones != 24 {
		t.Fatalf("expected /24 mask, got /%d", ones)
	}
	if !ipnet.IP.Equal(net.ParseIP("192.0.2.0").To4()) {
		t.Fatalf("expected 192.0.2.0, got %s", ipnet.IP)
	}
}

func TestIsAllAddr(t *testing.T) {
	if !isAllAddr(nil) {
		t.Fatal("nil should be 'all'")
	}
	if !isAllAddr(&net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}) {
		t.Fatal("0.0.0.0/0 should be 'all'")
	}
	if !isAllAddr(&net.IPNet{IP: net.IPv6unspecified, Mask: net.CIDRMask(0, 128)}) {
		t.Fatal("::/0 should be 'all'")
	}
	if isAllAddr(&net.IPNet{IP: net.ParseIP("192.0.2.1"), Mask: net.CIDRMask(32, 32)}) {
		t.Fatal("192.0.2.1/32 should not be 'all'")
	}
}
