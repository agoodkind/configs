package opnsense

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// IPv4 hosts pass through unchanged.
		{"v4 plain", "https://192.168.1.1", "https://192.168.1.1"},
		{"v4 trailing slash", "https://192.168.1.1/", "https://192.168.1.1"},
		{"v4 with port", "https://192.168.1.1:8443", "https://192.168.1.1:8443"},
		{"v4 with path stripped", "https://192.168.1.1/api/", "https://192.168.1.1/api"},

		// Hostnames pass through unchanged.
		{"hostname", "https://opnsense.example.com", "https://opnsense.example.com"},
		{"hostname with port", "https://opnsense.example.com:8443", "https://opnsense.example.com:8443"},

		// Bare IPv6 hosts get bracketed (the bug we're fixing).
		{"v6 long", "https://3d06:bad:b01::1", "https://[3d06:bad:b01::1]"},
		{"v6 trailing slash", "https://3d06:bad:b01::1/", "https://[3d06:bad:b01::1]"},
		{"v6 short", "https://::1", "https://[::1]"},
		{"v6 with path stripped", "https://3d06:bad:b01::1/api/", "https://[3d06:bad:b01::1]/api"},

		// Already-bracketed IPv6 stays as-is (idempotent).
		{"v6 bracketed", "https://[3d06:bad:b01::1]", "https://[3d06:bad:b01::1]"},
		{"v6 bracketed with port", "https://[3d06:bad:b01::1]:8443", "https://[3d06:bad:b01::1]:8443"},

		// No scheme prefix still works.
		{"no scheme v6", "3d06:bad:b01::1", "[3d06:bad:b01::1]"},
		{"no scheme v4", "192.168.1.1", "192.168.1.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeBaseURL(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
