package bgp

import (
	"context"
	"net/netip"
	"testing"

	apipb "github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	bgppkt "github.com/osrg/gobgp/v4/pkg/packet/bgp"
)

func TestAllowedPath(t *testing.T) {
	t.Parallel()
	s := speakerWithRouters(t)

	tests := []struct {
		name    string
		peer    string
		prefix  string
		allowed bool
	}{
		{
			name:    "inside allocation",
			peer:    "fd00::2",
			prefix:  "fd00:100:0:1::/64",
			allowed: true,
		},
		{
			name:    "outside allocation",
			peer:    "fd00::2",
			prefix:  "fd00:100:0:4::/64",
			allowed: false,
		},
		{
			name:    "covering supernet",
			peer:    "fd00::2",
			prefix:  "fd00:100::/60",
			allowed: false,
		},
		{
			name:    "unknown peer",
			peer:    "fd00::99",
			prefix:  "fd00:100:0:1::/64",
			allowed: false,
		},
		{
			name:    "router without allocations",
			peer:    "fd00::3",
			prefix:  "fd00:100:0:1::/64",
			allowed: false,
		},
		{
			name:    "host inside allocation",
			peer:    "fd00::2",
			prefix:  "fd00:100:0:1::1/128",
			allowed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			prefix := netip.MustParsePrefix(test.prefix)
			if got := s.AllowedPath(test.peer, prefix); got != test.allowed {
				t.Fatalf("AllowedPath(%q, %s) = %t; want %t", test.peer, prefix, got, test.allowed)
			}
		})
	}
}

func TestAuditAdjRIBInReturnsUndeclaredPrefix(t *testing.T) {
	t.Parallel()
	s := speakerWithRouters(t)
	fake := s.server.(*fakeBGPServer)
	allowed := netip.MustParsePrefix("fd00:100:0:1::/64")
	undeclared := netip.MustParsePrefix("fd00:100:0:4::/64")
	fake.setAdjRIBIn("fd00::2", []listPathResponse{
		{prefix: mustNLRI(t, allowed), paths: []*apiutil.Path{{}}},
		{prefix: mustNLRI(t, undeclared), paths: []*apiutil.Path{{}}},
	})

	violations := s.AuditAdjRibIn(context.Background())
	if len(violations) != 1 {
		t.Fatalf("violation count = %d; want 1", len(violations))
	}
	violation := violations[0]
	if violation.Router != "router2" {
		t.Errorf("violation router = %q; want %q", violation.Router, "router2")
	}
	if violation.Peer != "fd00::2" {
		t.Errorf("violation peer = %q; want %q", violation.Peer, "fd00::2")
	}
	if violation.Prefix != undeclared {
		t.Errorf("violation prefix = %s; want %s", violation.Prefix, undeclared)
	}
	if len(fake.listPathReqs) != 2 {
		t.Fatalf("ListPath call count = %d; want 2", len(fake.listPathReqs))
	}
	expectedPeers := make(map[string]bool, len(s.cfg.Routers))
	for _, router := range s.cfg.Routers {
		expectedPeers[router.AddressV6] = true
	}
	for _, request := range fake.listPathReqs {
		if !expectedPeers[request.Name] {
			t.Errorf("ListPath peer = %q; want a configured IPv6 peer", request.Name)
		}
		delete(expectedPeers, request.Name)
		if request.TableType != apipb.TableType_TABLE_TYPE_ADJ_IN {
			t.Errorf("ListPath table type = %s; want ADJ_IN", request.TableType)
		}
		if request.Family != bgppkt.RF_IPv6_UC {
			t.Errorf("ListPath family = %s; want IPv6 unicast", request.Family)
		}
	}
	if len(expectedPeers) != 0 {
		t.Errorf("ListPath skipped configured IPv6 peers: %v", expectedPeers)
	}
}

func speakerWithRouters(t *testing.T) *Speaker {
	t.Helper()
	fake := newFakeBGPServer()
	cfg := Config{
		Routers: []Router{
			{
				Name:      "router2",
				AddressV4: "10.0.0.2",
				AddressV6: "fd00::2",
				AllocationsV6: []netip.Prefix{
					netip.MustParsePrefix("fd00:100::/62"),
				},
			},
			{Name: "empty", AddressV4: "10.0.0.3", AddressV6: "fd00::3"},
		},
	}
	s := newSpeakerWithFake(cfg, fake)
	s.server = fake
	return s
}

func mustNLRI(t *testing.T, prefix netip.Prefix) bgppkt.NLRI {
	t.Helper()
	nlri, err := bgppkt.NewIPAddrPrefix(prefix)
	if err != nil {
		t.Fatalf("NewIPAddrPrefix(%s): %v", prefix, err)
	}
	return nlri
}
