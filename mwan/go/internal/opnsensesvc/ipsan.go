package opnsensesvc

import (
	"errors"
	"fmt"
	"net"
)

// parseIPSANs converts a slice of address strings into net.IP values.
// Empty input returns a nil slice without error.
func parseIPSANs(ipSANs []string) ([]net.IP, error) {
	if len(ipSANs) == 0 {
		return nil, nil
	}
	out := make([]net.IP, 0, len(ipSANs))
	for _, s := range ipSANs {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("ipsan: %q: %w", s, errors.New("invalid IP"))
		}
		out = append(out, ip)
	}
	return out, nil
}
