//go:build linux

package netif

import "testing"

func TestParseMonitorLine(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		family    string
		iface     string
		wantKind  EventKind
		wantDest  string
		wantVia   string
		wantCIDR  string
	}{
		{
			name:     "v6 default add",
			in:       "default via fe80::f61e:57ff:fe06:4983 dev mbrains proto ra metric 1024 pref high",
			family:   "inet6", iface: "mbrains",
			wantKind: EvRouteAdded, wantDest: "default", wantVia: "fe80::f61e:57ff:fe06:4983",
		},
		{
			name:     "v6 default delete",
			in:       "Deleted default via fe80::f61e:57ff:fe06:4983 dev mbrains proto ra metric 1024 pref high",
			family:   "inet6", iface: "mbrains",
			wantKind: EvRouteDeleted, wantDest: "default", wantVia: "fe80::f61e:57ff:fe06:4983",
		},
		{
			name:     "default on different iface ignored",
			in:       "default via 10.250.0.1 dev vmbr0",
			family:   "inet", iface: "mbrains",
			wantKind: EvUnknown,
		},
		{
			name:     "v4 addr add",
			in:       "3: mbrains    inet 158.247.70.13/26 brd 158.247.70.63 scope global mbrains",
			family:   "inet", iface: "mbrains",
			wantKind: EvAddrAdded, wantCIDR: "158.247.70.13/26",
		},
		{
			name:     "v6 SLAAC addr add",
			in:       "10: mbrains    inet6 2607:f598:d3e0:131:6662:66ff:fe23:f982/64 scope global dynamic mngtmpaddr ",
			family:   "inet6", iface: "mbrains",
			wantKind: EvAddrAdded, wantCIDR: "2607:f598:d3e0:131:6662:66ff:fe23:f982/64",
		},
		{
			name:     "addr delete",
			in:       "Deleted 3: mbrains    inet 158.247.70.13/26 brd 158.247.70.63 scope global mbrains",
			family:   "inet", iface: "mbrains",
			wantKind: EvAddrDeleted, wantCIDR: "158.247.70.13/26",
		},
		{
			name:     "addr on different iface ignored",
			in:       "5: vmbr0    inet 10.250.0.254/24 scope global vmbr0",
			family:   "inet", iface: "mbrains",
			wantKind: EvUnknown,
		},
		{
			name:     "junk line",
			in:       "what is this",
			family:   "inet6", iface: "mbrains",
			wantKind: EvUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMonitorLine(tc.in, tc.family, tc.iface)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind got %s want %s; raw=%q",
					got.Kind, tc.wantKind, tc.in)
			}
			if got.Dest != tc.wantDest {
				t.Errorf("dest got %q want %q", got.Dest, tc.wantDest)
			}
			if got.Via != tc.wantVia {
				t.Errorf("via got %q want %q", got.Via, tc.wantVia)
			}
			if got.CIDR != tc.wantCIDR {
				t.Errorf("cidr got %q want %q", got.CIDR, tc.wantCIDR)
			}
		})
	}
}

func TestIsAddrLine(t *testing.T) {
	if !isAddrLine("3: mbrains") {
		t.Fatal("expected addr line")
	}
	if isAddrLine("default via foo") {
		t.Fatal("default should not be addr line")
	}
	if isAddrLine("") {
		t.Fatal("empty should not be addr line")
	}
}
