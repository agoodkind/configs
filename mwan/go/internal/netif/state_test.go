//go:build linux

package netif

import (
	"reflect"
	"testing"
)

func TestParseAddrBriefList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []CurrentAddr
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "single iface multiple addrs",
			in:   "mbrains          UP             158.247.70.13/26 192.168.1.2/24 3d06:bad:b01:ff::1/128 fe80::6662:66ff:fe23:f982/64\n",
			want: []CurrentAddr{
				{CIDR: "158.247.70.13/26", Family: "inet"},
				{CIDR: "192.168.1.2/24", Family: "inet"},
				{CIDR: "3d06:bad:b01:ff::1/128", Family: "inet6"},
				{CIDR: "fe80::6662:66ff:fe23:f982/64", Family: "inet6"},
			},
		},
		{
			name: "down iface no addrs",
			in:   "mbrains          DOWN\n",
			want: nil,
		},
		{
			name: "ignores tokens without slash",
			in:   "mbrains UP 158.247.70.13/26 brd somejunk 3d06:bad:b01:ff::1/128\n",
			want: []CurrentAddr{
				{CIDR: "158.247.70.13/26", Family: "inet"},
				{CIDR: "3d06:bad:b01:ff::1/128", Family: "inet6"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAddrBriefList(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseAddrBriefList got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestParseRouteLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want *CurrentRoute
	}{
		{
			name: "v6 RA default",
			in:   "default via fe80::f61e:57ff:fe06:4983 dev mbrains proto ra metric 1024 pref high",
			want: &CurrentRoute{Dest: "default", Via: "fe80::f61e:57ff:fe06:4983", Dev: "mbrains", Metric: 1024},
		},
		{
			name: "v4 default no metric",
			in:   "default via 158.247.70.1 dev mbrains",
			want: &CurrentRoute{Dest: "default", Via: "158.247.70.1", Dev: "mbrains"},
		},
		{
			name: "blank",
			in:   "",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRouteLine(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseRouteLine got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestNormalizeCIDR(t *testing.T) {
	if got := normalizeCIDR("3D06:BAD:B01:FF::1/128"); got != "3d06:bad:b01:ff::1/128" {
		t.Fatalf("normalizeCIDR got %q", got)
	}
}

func TestAddrSpecFamilyInferred(t *testing.T) {
	if (AddrSpec{CIDR: "10.0.0.1/24"}).family() != "inet" {
		t.Fatal("v4 should be inet")
	}
	if (AddrSpec{CIDR: "::1/128"}).family() != "inet6" {
		t.Fatal("v6 should be inet6")
	}
	if (AddrSpec{CIDR: "::1", Family: "inet"}).family() != "inet" {
		t.Fatal("explicit override should win")
	}
}
