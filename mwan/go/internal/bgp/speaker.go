package bgp

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	apipb "github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	bgppkt "github.com/osrg/gobgp/v4/pkg/packet/bgp"
	"github.com/osrg/gobgp/v4/pkg/server"
)

// PeerState represents the BGP session state of a single neighbor.
type PeerState struct {
	Address     string
	AFI         string // "ipv4" or "ipv6"
	State       string // "ESTABLISHED", "ACTIVE", "IDLE", etc.
	Established bool
	UpSince     int64
}

// Status holds aggregate BGP status.
type Status struct {
	Announcing bool
	Peers      []PeerState
}

// Speaker wraps a GoBGP embedded server for programmatic route control.
type Speaker struct {
	cfg    Config
	log    *slog.Logger
	server *server.BgpServer

	mu         sync.Mutex
	announcing bool
	started    bool
}

// New creates a BGP speaker. Call Start to begin peering.
func New(cfg Config, log *slog.Logger) *Speaker {
	return &Speaker{
		cfg: cfg,
		log: log,
	}
}

// Start initializes the GoBGP server and configures all peers.
func (s *Speaker) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	logLevel := new(slog.LevelVar)
	logLevel.Set(slog.LevelInfo)
	s.server = server.NewBgpServer(
		server.LoggerOption(s.log, logLevel),
	)
	go s.server.Serve()

	if err := s.server.StartBgp(ctx, &apipb.StartBgpRequest{
		Global: &apipb.Global{
			Asn:        s.cfg.ASN,
			RouterId:   s.cfg.RouterID,
			ListenPort: s.cfg.ListenPort,
		},
	}); err != nil {
		return fmt.Errorf("start bgp: %w", err)
	}

	for _, n := range s.cfg.Neighbors {
		if err := s.addPeer(ctx, n.Address, false); err != nil {
			return fmt.Errorf("add peer %s: %w", n.Address, err)
		}
	}

	for _, n := range s.cfg.NeighborsV6 {
		if err := s.addPeer(ctx, n.Address, true); err != nil {
			return fmt.Errorf("add peer %s: %w", n.Address, err)
		}
	}

	// Watch for peer state changes. When all peers reach ESTABLISHED,
	// announce default routes automatically. No polling, no timeout.
	if err := s.server.WatchEvent(ctx, server.WatchEventMessageCallbacks{
		OnPeerUpdate: func(ev *apiutil.WatchEventMessage_PeerEvent, _ time.Time) {
			if ev.Type != apiutil.PEER_EVENT_STATE {
				return
			}
			if ev.Peer.State.SessionState != bgppkt.BGP_FSM_ESTABLISHED {
				return
			}
			s.log.Info("bgp peer established", "peer", ev.Peer.State.NeighborAddress)
			if s.IsEstablished() {
				if err := s.AnnounceDefault(); err != nil {
					s.log.Error("bgp auto-announce failed", "error", err)
				} else {
					s.log.Info("bgp routes announced (all peers established)")
				}
			}
		},
	}, server.WatchPeer()); err != nil {
		s.log.Error("bgp watch event registration failed", "error", err)
	}

	s.started = true
	s.log.Info("bgp speaker started",
		"asn", s.cfg.ASN,
		"router_id", s.cfg.RouterID,
		"port", s.cfg.ListenPort,
	)
	return nil
}

func (s *Speaker) addPeer(ctx context.Context, addr string, ipv6 bool) error {
	peer := &apipb.Peer{
		Conf: &apipb.PeerConf{
			NeighborAddress: addr,
			PeerAsn:         s.cfg.ASN, // iBGP
		},
		Timers: &apipb.Timers{
			Config: &apipb.TimersConfig{
				KeepaliveInterval: uint64(s.cfg.KeepaliveSeconds),
				HoldTime:          uint64(s.cfg.HoldSeconds),
			},
		},
		AfiSafis: []*apipb.AfiSafi{},
	}

	if ipv6 {
		peer.AfiSafis = append(peer.AfiSafis, &apipb.AfiSafi{
			Config: &apipb.AfiSafiConfig{
				Family: &apipb.Family{
					Afi:  apipb.Family_AFI_IP6,
					Safi: apipb.Family_SAFI_UNICAST,
				},
				Enabled: true,
			},
		})
	} else {
		peer.AfiSafis = append(peer.AfiSafis, &apipb.AfiSafi{
			Config: &apipb.AfiSafiConfig{
				Family: &apipb.Family{
					Afi:  apipb.Family_AFI_IP,
					Safi: apipb.Family_SAFI_UNICAST,
				},
				Enabled: true,
			},
		})
	}

	return s.server.AddPeer(ctx, &apipb.AddPeerRequest{Peer: peer})
}

// Stop gracefully shuts down the BGP server.
func (s *Speaker) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	ctx := context.Background()
	if err := s.server.StopBgp(ctx, &apipb.StopBgpRequest{}); err != nil {
		return fmt.Errorf("stop bgp: %w", err)
	}
	s.started = false
	s.announcing = false
	s.log.Info("bgp speaker stopped")
	return nil
}

// AnnounceDefault injects the configured default routes into BGP.
func (s *Speaker) AnnounceDefault() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.announcing {
		return nil
	}

	for _, prefix := range s.cfg.Announce.IPv4 {
		if err := s.addIPv4Path(prefix); err != nil {
			return fmt.Errorf("announce %s: %w", prefix, err)
		}
	}

	for _, prefix := range s.cfg.Announce.IPv6 {
		if err := s.addIPv6Path(prefix); err != nil {
			return fmt.Errorf("announce %s: %w", prefix, err)
		}
	}

	s.announcing = true
	s.log.Info("bgp routes announced",
		"ipv4", s.cfg.Announce.IPv4,
		"ipv6", s.cfg.Announce.IPv6,
	)
	return nil
}

// WithdrawDefault removes the configured default routes from BGP.
func (s *Speaker) WithdrawDefault() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.announcing {
		return nil
	}

	for _, prefix := range s.cfg.Announce.IPv4 {
		if err := s.deleteIPv4Path(prefix); err != nil {
			return fmt.Errorf("withdraw %s: %w", prefix, err)
		}
	}

	for _, prefix := range s.cfg.Announce.IPv6 {
		if err := s.deleteIPv6Path(prefix); err != nil {
			return fmt.Errorf("withdraw %s: %w", prefix, err)
		}
	}

	s.announcing = false
	s.log.Info("bgp routes withdrawn")
	return nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	pfx, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("parse prefix %q: %w", s, err)
	}
	return pfx, nil
}

func (s *Speaker) addIPv4Path(prefix string) error {
	pfx, err := parsePrefix(prefix)
	if err != nil {
		return err
	}

	nlri, err := bgppkt.NewIPAddrPrefix(pfx)
	if err != nil {
		return fmt.Errorf("new ipaddr prefix: %w", err)
	}

	routerID, err := netip.ParseAddr(s.cfg.RouterID)
	if err != nil {
		return fmt.Errorf("parse router id: %w", err)
	}

	nh, err := bgppkt.NewPathAttributeNextHop(routerID)
	if err != nil {
		return fmt.Errorf("new nexthop: %w", err)
	}

	origin := bgppkt.NewPathAttributeOrigin(0) // IGP

	path := &apiutil.Path{
		Family: bgppkt.RF_IPv4_UC,
		Nlri:   nlri,
		Attrs:  []bgppkt.PathAttributeInterface{origin, nh},
	}

	_, err = s.server.AddPath(apiutil.AddPathRequest{
		Paths: []*apiutil.Path{path},
	})
	return err
}

func (s *Speaker) deleteIPv4Path(prefix string) error {
	pfx, err := parsePrefix(prefix)
	if err != nil {
		return err
	}

	nlri, err := bgppkt.NewIPAddrPrefix(pfx)
	if err != nil {
		return fmt.Errorf("new ipaddr prefix: %w", err)
	}

	path := &apiutil.Path{
		Family:     bgppkt.RF_IPv4_UC,
		Nlri:       nlri,
		Withdrawal: true,
	}

	return s.server.DeletePath(apiutil.DeletePathRequest{
		Paths: []*apiutil.Path{path},
	})
}

func (s *Speaker) addIPv6Path(prefix string) error {
	pfx, err := parsePrefix(prefix)
	if err != nil {
		return err
	}

	nlri, err := bgppkt.NewIPAddrPrefix(pfx)
	if err != nil {
		return fmt.Errorf("new ipaddr prefix: %w", err)
	}

	// Use explicit IPv6 next-hop if configured, otherwise fall back to RouterID.
	// RFC 2545 requires an IPv6 next-hop for IPv6 unicast MP_REACH_NLRI.
	// GoBGP will convert an IPv4 RouterID to ::ffff:x.x.x.x which some FRR
	// versions may not install as a usable route.
	nhStr := s.cfg.NextHopV6
	if nhStr == "" {
		nhStr = s.cfg.RouterID
	}
	nextHop, err := netip.ParseAddr(nhStr)
	if err != nil {
		return fmt.Errorf("parse ipv6 next-hop %q: %w", nhStr, err)
	}

	origin := bgppkt.NewPathAttributeOrigin(0) // IGP

	mpReach, err := bgppkt.NewPathAttributeMpReachNLRI(
		bgppkt.RF_IPv6_UC,
		[]bgppkt.PathNLRI{{NLRI: nlri}},
		nextHop,
	)
	if err != nil {
		return fmt.Errorf("new mp reach: %w", err)
	}

	path := &apiutil.Path{
		Family: bgppkt.RF_IPv6_UC,
		Nlri:   nlri,
		Attrs:  []bgppkt.PathAttributeInterface{origin, mpReach},
	}

	_, err = s.server.AddPath(apiutil.AddPathRequest{
		Paths: []*apiutil.Path{path},
	})
	return err
}

func (s *Speaker) deleteIPv6Path(prefix string) error {
	pfx, err := parsePrefix(prefix)
	if err != nil {
		return err
	}

	nlri, err := bgppkt.NewIPAddrPrefix(pfx)
	if err != nil {
		return fmt.Errorf("new ipaddr prefix: %w", err)
	}

	path := &apiutil.Path{
		Family:     bgppkt.RF_IPv6_UC,
		Nlri:       nlri,
		Withdrawal: true,
	}

	return s.server.DeletePath(apiutil.DeletePathRequest{
		Paths: []*apiutil.Path{path},
	})
}

// Status returns the current state of all BGP peers.
func (s *Speaker) Status() Status {
	s.mu.Lock()
	announcing := s.announcing
	started := s.started
	s.mu.Unlock()

	st := Status{Announcing: announcing}
	if !started {
		return st
	}

	ctx := context.Background()
	err := s.server.ListPeer(ctx, &apipb.ListPeerRequest{}, func(p *apipb.Peer) {
		ps := PeerState{
			Address: p.Conf.NeighborAddress,
			State:   p.State.SessionState.String(),
		}

		if p.State.SessionState == apipb.PeerState_SESSION_STATE_ESTABLISHED {
			ps.Established = true
			if p.Timers != nil && p.Timers.State != nil {
				ps.UpSince = int64(p.Timers.State.Uptime.GetSeconds())
			}
		}

		for _, af := range p.AfiSafis {
			if af.Config != nil && af.Config.Family != nil {
				if af.Config.Family.Afi == apipb.Family_AFI_IP6 {
					ps.AFI = "ipv6"
				} else {
					ps.AFI = "ipv4"
				}
			}
		}
		if ps.AFI == "" {
			ps.AFI = "ipv4"
		}

		st.Peers = append(st.Peers, ps)
	})

	if err != nil {
		s.log.Error("list peers failed", "error", err)
	}

	return st
}

// IsEstablished returns true when all configured peers are in ESTABLISHED state.
func (s *Speaker) IsEstablished() bool {
	st := s.Status()
	if len(st.Peers) == 0 {
		return false
	}
	for _, p := range st.Peers {
		if !p.Established {
			return false
		}
	}
	return true
}
