//go:build linux

package wanconfig

import (
	"errors"
	"net/netip"
	"slices"
	"testing"
)

// TestConfigItems_DescribesEveryMemberAndTranslation pins the published shape
// for a gateway like the testbed's: three members, one fallback, every member
// probed, two carrying a translation pair. Every path here is one a RESTCONF
// reader sees, so a change to this list is a change to the served tree.
func TestConfigItems_DescribesEveryMemberAndTranslation(t *testing.T) {
	t.Parallel()
	internal := netip.MustParsePrefix("3d06:bad:b01:210::/60")
	gateway := Gateway{
		InternalIface: "eninternal0",
		Members: []Member{
			{
				Name: "att", Iface: "enatt0.3242", Tier: 0, ProbePolicy: "att",
				NPTInternal: internal, NPTExternal: netip.MustParsePrefix("2001:db8:a::/60"),
			},
			{Name: "monkeybrains", Iface: "enmbrains0", Tier: 1, ProbePolicy: "monkeybrains"},
			{
				Name: "webpass", Iface: "enwebpass0", Tier: 0, ProbePolicy: "webpass",
				NPTInternal: internal, NPTExternal: netip.MustParsePrefix("2001:db8:b::/60"),
			},
		},
	}

	items, err := ConfigItems(gateway)
	if err != nil {
		t.Fatalf("ConfigItems: %v", err)
	}

	want := []Item{
		{Path: "/ietf-interfaces:interfaces/interface[name='eninternal0']/type", Value: "iana-if-type:other"},
		{Path: "/ietf-interfaces:interfaces/interface[name='eninternal0']/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='eninternal0']/ietf-ip:ipv4/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='eninternal0']/ietf-ip:ipv6/enabled", Value: "true"},

		{Path: "/ietf-interfaces:interfaces/interface[name='enatt0.3242']/type", Value: "iana-if-type:other"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enatt0.3242']/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enatt0.3242']/ietf-ip:ipv4/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enatt0.3242']/ietf-ip:ipv6/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enatt0.3242']/goodkind-mwan-steering:steering/tier", Value: "0"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enatt0.3242']/goodkind-mwan-steering:steering/probe-policy", Value: "att"},

		{Path: "/ietf-interfaces:interfaces/interface[name='enmbrains0']/type", Value: "iana-if-type:other"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enmbrains0']/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enmbrains0']/ietf-ip:ipv4/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enmbrains0']/ietf-ip:ipv6/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enmbrains0']/goodkind-mwan-steering:steering/tier", Value: "1"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enmbrains0']/goodkind-mwan-steering:steering/probe-policy", Value: "monkeybrains"},

		{Path: "/ietf-interfaces:interfaces/interface[name='enwebpass0']/type", Value: "iana-if-type:other"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enwebpass0']/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enwebpass0']/ietf-ip:ipv4/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enwebpass0']/ietf-ip:ipv6/enabled", Value: "true"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enwebpass0']/goodkind-mwan-steering:steering/tier", Value: "0"},
		{Path: "/ietf-interfaces:interfaces/interface[name='enwebpass0']/goodkind-mwan-steering:steering/probe-policy", Value: "webpass"},

		{Path: "/ietf-nat:nat/instances/instance[id='1']/name", Value: "att"},
		{Path: "/ietf-nat:nat/instances/instance[id='1']/type", Value: "ietf-nat:nptv6"},
		{Path: "/ietf-nat:nat/instances/instance[id='1']/enable", Value: "true"},
		{Path: "/ietf-nat:nat/instances/instance[id='1']/policy[id='1']/nptv6-prefixes[internal-ipv6-prefix='3d06:bad:b01:210::/60']/external-ipv6-prefix", Value: "2001:db8:a::/60"},
		{Path: "/ietf-nat:nat/instances/instance[id='2']/name", Value: "webpass"},
		{Path: "/ietf-nat:nat/instances/instance[id='2']/type", Value: "ietf-nat:nptv6"},
		{Path: "/ietf-nat:nat/instances/instance[id='2']/enable", Value: "true"},
		{Path: "/ietf-nat:nat/instances/instance[id='2']/policy[id='1']/nptv6-prefixes[internal-ipv6-prefix='3d06:bad:b01:210::/60']/external-ipv6-prefix", Value: "2001:db8:b::/60"},
	}
	if !slices.Equal(items, want) {
		t.Fatalf("items differ\n got: %v\nwant: %v", items, want)
	}
}

// TestConfigItems_LeavesUnprobedMemberWithoutPolicy pins that a member the
// daemon runs no probe for gets a tier and no probe-policy leaf, rather than an
// empty string the schema would accept and a reader would misread.
func TestConfigItems_LeavesUnprobedMemberWithoutPolicy(t *testing.T) {
	t.Parallel()
	items, err := ConfigItems(Gateway{
		InternalIface: "eninternal0",
		Members:       []Member{{Name: "att", Iface: "enatt0", Tier: 0}},
	})
	if err != nil {
		t.Fatalf("ConfigItems: %v", err)
	}
	for _, item := range items {
		if item.Path == "/ietf-interfaces:interfaces/interface[name='enatt0']/goodkind-mwan-steering:steering/probe-policy" {
			t.Fatalf("probe-policy published for an unprobed member: %v", item)
		}
	}
	if len(items) != 4+4+1 {
		t.Fatalf("item count = %d, want 9", len(items))
	}
}

// TestConfigItems_RejectsWhatAPathCannotCarry pins the failure contract: an
// unpublishable gateway returns ErrInvalidGateway and no items, so the caller
// logs and keeps running rather than writing a partial tree.
func TestConfigItems_RejectsWhatAPathCannotCarry(t *testing.T) {
	t.Parallel()
	internal := netip.MustParsePrefix("3d06:bad:b01:210::/60")
	cases := map[string]Gateway{
		"empty internal link": {InternalIface: "", Members: nil},
		"empty member name":   {InternalIface: "eninternal0", Members: []Member{{Name: "", Iface: "enatt0"}}},
		"quote in link":       {InternalIface: "eninternal0", Members: []Member{{Name: "att", Iface: "en'att0"}}},
		"duplicate link": {InternalIface: "eninternal0", Members: []Member{
			{Name: "att", Iface: "enatt0"}, {Name: "webpass", Iface: "enatt0"},
		}},
		"member on the internal link": {InternalIface: "eninternal0", Members: []Member{{Name: "att", Iface: "eninternal0"}}},
		"one translation prefix":      {InternalIface: "eninternal0", Members: []Member{{Name: "att", Iface: "enatt0", NPTInternal: internal}}},
		"ipv4 translation prefix": {InternalIface: "eninternal0", Members: []Member{{
			Name: "att", Iface: "enatt0",
			NPTInternal: internal, NPTExternal: netip.MustParsePrefix("10.0.0.0/8"),
		}}},
	}
	for name, gateway := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			items, err := ConfigItems(gateway)
			if !errors.Is(err, ErrInvalidGateway) {
				t.Fatalf("err = %v, want ErrInvalidGateway", err)
			}
			if items != nil {
				t.Fatalf("items = %v, want none", items)
			}
		})
	}
}
