//go:build linux

package netif

import "testing"

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

func TestFamilyToNetlink(t *testing.T) {
	if got := familyToNetlink("inet"); got == 0 {
		t.Fatal("inet should map to AF_INET, not 0")
	}
	if got := familyToNetlink("inet6"); got == 0 {
		t.Fatal("inet6 should map to AF_INET6, not 0")
	}
	if familyToNetlink("inet") == familyToNetlink("inet6") {
		t.Fatal("inet and inet6 must map to distinct constants")
	}
}
