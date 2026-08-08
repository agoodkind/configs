package config

import (
	"net/netip"
	"testing"
)

func TestValidateBGPAcceptsRouterOnly(t *testing.T) {
	addressV4 := netip.AddrFrom4([4]byte{}).String()
	addressV6 := netip.AddrFrom16([16]byte{}).String()
	allocation := netip.PrefixFrom(netip.AddrFrom16([16]byte{}), 0).String()
	routers := []BGPRouter{{
		Name:          t.Name(),
		AddressV4:     addressV4,
		AddressV6:     addressV6,
		AllocationsV6: []string{allocation},
	}}
	keepalive := uint32(len(routers))
	b := BGPSection{
		ASN:              keepalive,
		RouterID:         routers[0].AddressV4,
		KeepaliveSeconds: keepalive,
		HoldSeconds:      3 * keepalive,
		ListenPort:       int32(len(routers)),
		Routers:          routers,
		Announce:         BGPAnnounce{IPv6: routers[0].AllocationsV6},
	}

	if err := validateBGP(&b, allocation); err != nil {
		t.Fatalf("router-only BGP config must pass validation: %v", err)
	}
}

func TestValidateBGPRoutersRejectsOverlap(t *testing.T) {
	tests := []struct {
		name    string
		routers []BGPRouter
	}{
		{
			name: "overlapping allocations",
			routers: []BGPRouter{
				{
					Name:          "opnsense",
					AddressV4:     "10.250.250.2",
					AddressV6:     "3d06:bad:b01:fe::2",
					AllocationsV6: []string{"3d06:bad:b01::/61"},
				},
				{
					Name:          "router2",
					AddressV4:     "10.250.250.5",
					AddressV6:     "3d06:bad:b01:fe::5",
					AllocationsV6: []string{"3d06:bad:b01:4::/62"},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateBGPRouters(test.routers, "3d06:bad:b01::/60"); err == nil {
				t.Fatal("overlapping allocations must fail validation")
			}
		})
	}
}

func TestValidateBGPRoutersRejectsOutsideBlock(t *testing.T) {
	tests := []struct {
		name       string
		allocation string
	}{
		{name: "outside IPv6 block", allocation: "2001:db8::/64"},
		{name: "larger than internal block", allocation: "3d06:bad:b01::/59"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routers := []BGPRouter{{
				Name:          "router",
				AddressV4:     "10.250.250.5",
				AddressV6:     "3d06:bad:b01:fe::5",
				AllocationsV6: []string{test.allocation},
			}}
			if err := validateBGPRouters(routers, "3d06:bad:b01::/60"); err == nil {
				t.Fatal("allocation outside the internal block must fail")
			}
		})
	}
}

func TestValidateBGPRoutersAcceptsDisjointAndHost(t *testing.T) {
	tests := []struct {
		name    string
		routers []BGPRouter
	}{
		{
			name: "prefix and host",
			routers: []BGPRouter{
				{
					Name:          "router-a",
					AddressV4:     "10.250.250.2",
					AddressV6:     "3d06:bad:b01:fe::2",
					AllocationsV6: []string{"3d06:bad:b01::/61"},
				},
				{
					Name:          "router-b",
					AddressV4:     "10.250.250.5",
					AddressV6:     "3d06:bad:b01:fe::5",
					AllocationsV6: []string{"3d06:bad:b01:8::1/128"},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateBGPRouters(test.routers, "3d06:bad:b01::/60"); err != nil {
				t.Fatalf("disjoint allocations must pass: %v", err)
			}
		})
	}
}

func TestValidateBGPRoutersRejectsMissingFields(t *testing.T) {
	tests := []struct {
		name   string
		router BGPRouter
	}{
		{
			name: "missing name",
			router: BGPRouter{
				AddressV4:     "10.250.250.2",
				AddressV6:     "3d06:bad:b01:fe::2",
				AllocationsV6: []string{"3d06:bad:b01::/61"},
			},
		},
		{
			name: "missing IPv4 address",
			router: BGPRouter{
				Name:          "router",
				AddressV6:     "3d06:bad:b01:fe::2",
				AllocationsV6: []string{"3d06:bad:b01::/61"},
			},
		},
		{
			name: "missing IPv6 address",
			router: BGPRouter{
				Name:          "router",
				AddressV4:     "10.250.250.2",
				AllocationsV6: []string{"3d06:bad:b01::/61"},
			},
		},
		{
			name: "missing allocations",
			router: BGPRouter{
				Name:          "router",
				AddressV4:     "10.250.250.2",
				AddressV6:     "3d06:bad:b01:fe::2",
				AllocationsV6: nil,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateBGPRouters([]BGPRouter{test.router}, "3d06:bad:b01::/60"); err == nil {
				t.Fatal("missing router fields must fail validation")
			}
		})
	}
}

func TestValidateBGPRoutersRejectsDuplicateName(t *testing.T) {
	routers := []BGPRouter{
		{
			Name:          "router",
			AddressV4:     "10.250.250.2",
			AddressV6:     "3d06:bad:b01:fe::2",
			AllocationsV6: []string{"3d06:bad:b01::/61"},
		},
		{
			Name:          "router",
			AddressV4:     "10.250.250.5",
			AddressV6:     "3d06:bad:b01:fe::5",
			AllocationsV6: []string{"3d06:bad:b01:8::1/128"},
		},
	}

	if err := validateBGPRouters(routers, "3d06:bad:b01::/60"); err == nil {
		t.Fatal("duplicate router names must fail validation")
	}
}
