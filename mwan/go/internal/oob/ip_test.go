//go:build linux

package oob

import "testing"

func TestIsMutating(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"addr show", []string{"-br", "addr", "show", "dev", "mbrains"}, false},
		{"route show", []string{"-6", "route", "show", "table", "oob"}, false},
		{"rule show", []string{"-6", "rule", "show"}, false},
		{"monitor", []string{"-6", "monitor", "route"}, false},
		{"addr replace", []string{"-6", "addr", "replace", "::1/128", "dev", "mbrains"}, true},
		{"route del", []string{"-6", "route", "del", "default", "table", "oob"}, true},
		{"rule add", []string{"-6", "rule", "add", "from", "::1", "lookup", "oob"}, true},
		{"link set up", []string{"link", "set", "mbrains", "up"}, true},
		{"empty", []string{}, false},
		{"single", []string{"-6"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMutating(tc.args); got != tc.want {
				t.Fatalf("isMutating(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
