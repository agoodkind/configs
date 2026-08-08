package bgp

import (
	"context"
	"net/netip"

	apipb "github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	bgppkt "github.com/osrg/gobgp/v4/pkg/packet/bgp"
)

// Violation identifies an announcement outside a router's allocation.
type Violation struct {
	Router string
	Peer   string
	Prefix netip.Prefix
}

// AllowedPath reports whether a configured peer may announce prefix.
func (s *Speaker) AllowedPath(peerAddr string, prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return false
	}
	router := s.routerForPeer(peerAddr)
	if router == nil {
		return false
	}
	for _, allocation := range router.AllocationsV6 {
		if allocation.Contains(prefix.Addr()) && prefix.Bits() >= allocation.Bits() {
			return true
		}
	}
	return false
}

// AuditAdjRibIn returns every configured peer path outside its allocation.
func (s *Speaker) AuditAdjRibIn(ctx context.Context) []Violation {
	violations := []Violation{}
	if s.server == nil {
		return violations
	}
	for routerIndex := range s.cfg.Routers {
		router := &s.cfg.Routers[routerIndex]
		// Only the IPv6 peer negotiates IPv6 unicast and carries allocations.
		peerAddr := router.AddressV6
		err := s.server.ListPath(apiutil.ListPathRequest{
			TableType: apipb.TableType_TABLE_TYPE_ADJ_IN,
			Name:      peerAddr,
			Family:    bgppkt.RF_IPv6_UC,
		}, func(nlri bgppkt.NLRI, paths []*apiutil.Path) {
			prefix, err := netip.ParsePrefix(nlri.String())
			if err != nil {
				s.log.WarnContext(ctx, "parse adj-rib-in prefix failed", "peer", peerAddr, "error", err)
				return
			}
			if s.AllowedPath(peerAddr, prefix) {
				return
			}
			for range paths {
				violations = append(violations, Violation{
					Router: router.Name,
					Peer:   peerAddr,
					Prefix: prefix,
				})
			}
		})
		if err != nil {
			s.log.ErrorContext(ctx, "list adj-rib-in failed", "peer", peerAddr, "error", err)
		}
	}
	return violations
}
