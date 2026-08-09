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

// bgpServerAPI is the narrow surface of *server.BgpServer that Speaker
// calls into. It exists so tests can swap in a fake and assert the
// GoBGP API requests we build (GR fields on Start, AllowGracefulRestart
// on Stop, MpGracefulRestart on AddPeer). The production constructor
// New wires the real *server.BgpServer; tests use newWithServer.
type bgpServerAPI interface {
	Serve()
	StartBgp(ctx context.Context, r *apipb.StartBgpRequest) error
	StopBgp(ctx context.Context, r *apipb.StopBgpRequest) error
	AddPeer(ctx context.Context, r *apipb.AddPeerRequest) error
	WatchEvent(ctx context.Context, callbacks server.WatchEventMessageCallbacks, opts ...server.WatchOption) error
	ListPeer(ctx context.Context, r *apipb.ListPeerRequest, fn func(*apipb.Peer)) error
	ListPath(r apiutil.ListPathRequest, fn func(prefix bgppkt.NLRI, paths []*apiutil.Path)) error
	AddPath(req apiutil.AddPathRequest) ([]apiutil.AddPathResponse, error)
	DeletePath(req apiutil.DeletePathRequest) error
}

// Speaker wraps a GoBGP embedded server for programmatic route control.
type Speaker struct {
	cfg    Config
	log    *slog.Logger
	server bgpServerAPI
	fib    *FIB

	// newServer constructs the underlying GoBGP server. Production code
	// leaves this nil so Start defaults to server.NewBgpServer; tests
	// override it to inject a fake bgpServerAPI before Start runs.
	newServer func(log *slog.Logger) bgpServerAPI

	mu         sync.Mutex
	announcing bool
	started    bool
	watchMu    sync.Mutex
	seenPeers  map[string]bool
	upPeers    map[string]bool
}

// New creates a BGP speaker. Call Start to begin peering.
func New(cfg Config, log *slog.Logger) *Speaker {
	return &Speaker{
		cfg:        cfg,
		log:        log,
		server:     nil,
		fib:        nil,
		newServer:  nil,
		mu:         sync.Mutex{},
		announcing: false,
		started:    false,
		watchMu:    sync.Mutex{},
		seenPeers:  make(map[string]bool),
		upPeers:    make(map[string]bool),
	}
}

// SetFIB attaches the learned-route consumer before the speaker starts.
func (s *Speaker) SetFIB(fib *FIB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fib = fib
}

// Start initializes the GoBGP server and configures all peers.
func (s *Speaker) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	if s.newServer != nil {
		s.server = s.newServer(s.log)
	} else {
		logLevel := new(slog.LevelVar)
		logLevel.Set(slog.LevelInfo)
		s.server = server.NewBgpServer(
			server.LoggerOption(s.log, logLevel),
		)
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				recoveredErr := fmt.Errorf("panic: %v", recovered)
				s.log.ErrorContext(ctx, "bgp server panic", "error", recoveredErr)
			}
		}()
		s.server.Serve()
	}()

	global := &apipb.Global{
		Asn:        s.cfg.ASN,
		RouterId:   s.cfg.RouterID,
		ListenPort: s.cfg.ListenPort,
	}
	if s.cfg.GracefulRestart.Enabled {
		global.GracefulRestart = &apipb.GracefulRestart{
			Enabled:             true,
			RestartTime:         s.cfg.GracefulRestart.RestartTime,
			NotificationEnabled: s.cfg.GracefulRestart.NotificationEnabled,
		}
	}
	if err := s.server.StartBgp(ctx, &apipb.StartBgpRequest{Global: global}); err != nil {
		s.log.ErrorContext(ctx, "start bgp failed", "error", err)
		return fmt.Errorf("start bgp: %w", err)
	}

	if err := s.addConfiguredPeers(ctx); err != nil {
		return err
	}

	// One watch keeps route installation and peer-loss cleanup ordered with
	// GoBGP's view of the session.
	if err := s.server.WatchEvent(ctx, server.WatchEventMessageCallbacks{
		OnPeerUpdate: func(event *apiutil.WatchEventMessage_PeerEvent, _ time.Time) {
			s.handlePeerUpdate(ctx, event)
		},
		OnBestPath: func(paths []*apiutil.Path, _ time.Time) {
			s.handleBestPaths(ctx, paths)
		},
	}, server.WatchPeer(), server.WatchBestPath(true)); err != nil {
		s.log.ErrorContext(ctx, "bgp watch event registration failed", "error", err)
	}

	s.started = true
	s.log.InfoContext(ctx, "bgp speaker started",
		"asn", s.cfg.ASN,
		"router_id", s.cfg.RouterID,
		"port", s.cfg.ListenPort,
	)
	return nil
}

func (s *Speaker) handlePeerUpdate(ctx context.Context, event *apiutil.WatchEventMessage_PeerEvent) {
	if event.Type != apiutil.PEER_EVENT_STATE {
		return
	}
	peer := event.Peer.State.NeighborAddress.String()
	if event.Peer.State.SessionState == bgppkt.BGP_FSM_ESTABLISHED {
		s.log.InfoContext(ctx, "bgp peer established", "peer", peer)
		s.markPeerEstablished(peer)
		if s.IsEstablished() {
			if err := s.AnnounceDefault(); err != nil {
				s.log.ErrorContext(ctx, "bgp auto-announce failed", "error", err)
			} else {
				s.log.InfoContext(ctx, "bgp routes announced (all peers established)")
			}
		}
		return
	}

	s.handlePeerDown(ctx, peer)
	if event.Peer.State.DisconnectReason == apipb.PeerState_DISCONNECT_REASON_HOLD_TIMER_EXPIRED {
		s.markPeerTimedOut(peer)
	}
}

func (s *Speaker) handleBestPaths(ctx context.Context, paths []*apiutil.Path) {
	if s.fib == nil {
		return
	}
	for _, path := range paths {
		if path == nil || path.Family != bgppkt.RF_IPv6_UC || path.Nlri == nil {
			continue
		}
		peer := path.PeerAddress.String()
		prefix, err := netip.ParsePrefix(path.Nlri.String())
		if err != nil {
			s.log.WarnContext(ctx, "ignore BGP best path with invalid prefix", "peer", peer, "error", err)
			continue
		}
		if !s.AllowedPath(peer, prefix) {
			s.log.WarnContext(ctx, "reject BGP best path outside router allocation", "peer", peer, "prefix", prefix)
			continue
		}
		event := PathEvent{
			Peer:      peer,
			Prefix:    prefix,
			NextHop:   netip.Addr{},
			Withdrawn: path.Withdrawal,
		}
		if !path.Withdrawal {
			nextHop, err := bestPathNextHop(path)
			if err != nil {
				s.log.WarnContext(ctx, "ignore BGP best path with invalid next hop", "peer", peer, "prefix", prefix, "error", err)
				continue
			}
			event.NextHop = nextHop
		}
		if err := s.fib.Apply(ctx, event); err != nil {
			s.log.ErrorContext(ctx, "reconcile BGP best path failed", "peer", peer, "prefix", prefix, "error", err)
		}
	}
}

func bestPathNextHop(path *apiutil.Path) (netip.Addr, error) {
	for _, attribute := range path.Attrs {
		switch value := attribute.(type) {
		case *bgppkt.PathAttributeMpReachNLRI:
			if value.Nexthop.IsValid() {
				return value.Nexthop, nil
			}
		case *bgppkt.PathAttributeNextHop:
			if value.Value.IsValid() {
				return value.Value, nil
			}
		}
	}
	return netip.Addr{}, fmt.Errorf("missing next hop")
}

func (s *Speaker) markPeerEstablished(peer string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	s.upPeers[peer] = true
	s.markPeerSeenLocked(peer)
}

func (s *Speaker) markPeerTimedOut(peer string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	s.markPeerSeenLocked(peer)
}

func (s *Speaker) markPeerSeenLocked(peer string) {
	router := s.routerForPeer(peer)
	if router == nil || peer != router.AddressV6 {
		return
	}
	s.seenPeers[peer] = true
	if len(s.seenPeers) != len(s.cfg.Routers) || s.fib == nil {
		return
	}
	s.fib.ArmSweep()
}

func (s *Speaker) handlePeerDown(ctx context.Context, peer string) {
	router := s.routerForPeer(peer)
	if router == nil || peer != router.AddressV6 || s.fib == nil {
		return
	}
	s.watchMu.Lock()
	wasUp := s.upPeers[peer]
	s.upPeers[peer] = false
	s.watchMu.Unlock()
	if !wasUp {
		return
	}
	for _, allocation := range router.AllocationsV6 {
		event := PathEvent{
			Peer:      peer,
			Prefix:    allocation,
			NextHop:   netip.Addr{},
			Withdrawn: true,
		}
		if err := s.fib.Apply(ctx, event); err != nil {
			s.log.ErrorContext(ctx, "withdraw BGP router allocation failed", "router", router.Name, "peer", peer, "prefix", allocation, "error", err)
		}
	}
}

func (s *Speaker) addConfiguredPeers(ctx context.Context) error {
	if len(s.cfg.Routers) == 0 {
		return s.addLegacyPeers(ctx)
	}
	return s.addRouterPeers(ctx)
}

func (s *Speaker) addRouterPeers(ctx context.Context) error {
	for _, router := range s.cfg.Routers {
		if err := s.addPeer(ctx, router.AddressV4, false); err != nil {
			s.log.ErrorContext(ctx, "add BGP peer failed", "peer", router.AddressV4, "error", err)
			return fmt.Errorf("add peer %s: %w", router.AddressV4, err)
		}
		if err := s.addPeer(ctx, router.AddressV6, true); err != nil {
			s.log.ErrorContext(ctx, "add BGP peer failed", "peer", router.AddressV6, "error", err)
			return fmt.Errorf("add peer %s: %w", router.AddressV6, err)
		}
	}
	return nil
}

func (s *Speaker) addLegacyPeers(ctx context.Context) error {
	for _, neighbor := range s.cfg.Neighbors {
		if err := s.addPeer(ctx, neighbor.Address, false); err != nil {
			s.log.ErrorContext(ctx, "add BGP peer failed", "peer", neighbor.Address, "error", err)
			return fmt.Errorf("add peer %s: %w", neighbor.Address, err)
		}
	}
	for _, neighbor := range s.cfg.NeighborsV6 {
		if err := s.addPeer(ctx, neighbor.Address, true); err != nil {
			s.log.ErrorContext(ctx, "add BGP peer failed", "peer", neighbor.Address, "error", err)
			return fmt.Errorf("add peer %s: %w", neighbor.Address, err)
		}
	}
	return nil
}

func (s *Speaker) routerForPeer(addr string) *Router {
	for routerIndex := range s.cfg.Routers {
		router := &s.cfg.Routers[routerIndex]
		if router.AddressV4 == addr || router.AddressV6 == addr {
			return router
		}
	}
	return nil
}

func (s *Speaker) addPeer(ctx context.Context, addr string, ipv6 bool) error {
	if router := s.routerForPeer(addr); router != nil {
		s.log.DebugContext(ctx, "add BGP router peer", "router", router.Name, "peer", addr)
	}
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

	if s.cfg.GracefulRestart.Enabled {
		peer.GracefulRestart = &apipb.GracefulRestart{
			Enabled:             true,
			RestartTime:         s.cfg.GracefulRestart.RestartTime,
			NotificationEnabled: s.cfg.GracefulRestart.NotificationEnabled,
		}
	}

	afiSafi := &apipb.AfiSafi{
		Config: &apipb.AfiSafiConfig{
			Family:  &apipb.Family{Safi: apipb.Family_SAFI_UNICAST},
			Enabled: true,
		},
	}
	if ipv6 {
		afiSafi.Config.Family.Afi = apipb.Family_AFI_IP6
	} else {
		afiSafi.Config.Family.Afi = apipb.Family_AFI_IP
	}
	if s.cfg.GracefulRestart.Enabled {
		afiSafi.MpGracefulRestart = &apipb.MpGracefulRestart{
			Config: &apipb.MpGracefulRestartConfig{Enabled: true},
		}
	}
	peer.AfiSafis = append(peer.AfiSafis, afiSafi)

	if err := s.server.AddPeer(ctx, &apipb.AddPeerRequest{Peer: peer}); err != nil {
		s.log.ErrorContext(ctx, "add bgp peer api failed", "peer", addr, "error", err)
		return fmt.Errorf("add peer: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the BGP server.
func (s *Speaker) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	ctx := context.Background()
	stopReq := &apipb.StopBgpRequest{}
	if s.cfg.GracefulRestart.Enabled {
		stopReq.AllowGracefulRestart = true
	}
	if err := s.server.StopBgp(ctx, stopReq); err != nil {
		s.log.ErrorContext(ctx, "stop bgp failed", "error", err)
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
			s.log.Error("announce bgp route failed", "prefix", prefix, "error", err)
			return fmt.Errorf("announce %s: %w", prefix, err)
		}
	}

	for _, prefix := range s.cfg.Announce.IPv6 {
		if err := s.addIPv6Path(prefix); err != nil {
			s.log.Error("announce bgp route failed", "prefix", prefix, "error", err)
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
			s.log.Error("withdraw bgp route failed", "prefix", prefix, "error", err)
			return fmt.Errorf("withdraw %s: %w", prefix, err)
		}
	}

	for _, prefix := range s.cfg.Announce.IPv6 {
		if err := s.deleteIPv6Path(prefix); err != nil {
			s.log.Error("withdraw bgp route failed", "prefix", prefix, "error", err)
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
		slog.Warn("bgp: parse prefix failed", "err", err, "input", s)
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
		s.log.Error("create ipv4 nlri failed", "prefix", prefix, "error", err)
		return fmt.Errorf("new ipaddr prefix: %w", err)
	}

	routerID, err := netip.ParseAddr(s.cfg.RouterID)
	if err != nil {
		s.log.Error("parse bgp router id failed", "router_id", s.cfg.RouterID, "error", err)
		return fmt.Errorf("parse router id: %w", err)
	}

	nh, err := bgppkt.NewPathAttributeNextHop(routerID)
	if err != nil {
		s.log.Error("create ipv4 nexthop failed", "router_id", s.cfg.RouterID, "error", err)
		return fmt.Errorf("new nexthop: %w", err)
	}

	origin := bgppkt.NewPathAttributeOrigin(0) // IGP

	path := &apiutil.Path{
		Family: bgppkt.RF_IPv4_UC,
		Nlri:   nlri,
		Attrs:  []bgppkt.PathAttributeInterface{origin, nh},
	}

	if _, err := s.server.AddPath(apiutil.AddPathRequest{
		Paths: []*apiutil.Path{path},
	}); err != nil {
		return fmt.Errorf("add ipv4 path: %w", err)
	}
	return nil
}

func (s *Speaker) deleteIPv4Path(prefix string) error {
	pfx, err := parsePrefix(prefix)
	if err != nil {
		return err
	}

	nlri, err := bgppkt.NewIPAddrPrefix(pfx)
	if err != nil {
		s.log.Error("create ipv4 nlri failed", "prefix", prefix, "error", err)
		return fmt.Errorf("new ipaddr prefix: %w", err)
	}

	path := &apiutil.Path{
		Family:     bgppkt.RF_IPv4_UC,
		Nlri:       nlri,
		Withdrawal: true,
	}

	if err := s.server.DeletePath(apiutil.DeletePathRequest{
		Paths: []*apiutil.Path{path},
	}); err != nil {
		return fmt.Errorf("delete ipv4 path: %w", err)
	}
	return nil
}

func (s *Speaker) addIPv6Path(prefix string) error {
	pfx, err := parsePrefix(prefix)
	if err != nil {
		return err
	}

	nlri, err := bgppkt.NewIPAddrPrefix(pfx)
	if err != nil {
		s.log.Error("create ipv6 nlri failed", "prefix", prefix, "error", err)
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
		s.log.Error("parse ipv6 nexthop failed", "next_hop", nhStr, "error", err)
		return fmt.Errorf("parse ipv6 next-hop %q: %w", nhStr, err)
	}

	origin := bgppkt.NewPathAttributeOrigin(0) // IGP

	mpReach, err := bgppkt.NewPathAttributeMpReachNLRI(
		bgppkt.RF_IPv6_UC,
		[]bgppkt.PathNLRI{{NLRI: nlri}},
		nextHop,
	)
	if err != nil {
		s.log.Error("create ipv6 mp reach failed", "prefix", prefix, "error", err)
		return fmt.Errorf("new mp reach: %w", err)
	}

	path := &apiutil.Path{
		Family: bgppkt.RF_IPv6_UC,
		Nlri:   nlri,
		Attrs:  []bgppkt.PathAttributeInterface{origin, mpReach},
	}

	if _, err := s.server.AddPath(apiutil.AddPathRequest{
		Paths: []*apiutil.Path{path},
	}); err != nil {
		return fmt.Errorf("add ipv6 path: %w", err)
	}
	return nil
}

func (s *Speaker) deleteIPv6Path(prefix string) error {
	pfx, err := parsePrefix(prefix)
	if err != nil {
		return err
	}

	nlri, err := bgppkt.NewIPAddrPrefix(pfx)
	if err != nil {
		s.log.Error("create ipv6 nlri failed", "prefix", prefix, "error", err)
		return fmt.Errorf("new ipaddr prefix: %w", err)
	}

	path := &apiutil.Path{
		Family:     bgppkt.RF_IPv6_UC,
		Nlri:       nlri,
		Withdrawal: true,
	}

	if err := s.server.DeletePath(apiutil.DeletePathRequest{
		Paths: []*apiutil.Path{path},
	}); err != nil {
		return fmt.Errorf("delete ipv6 path: %w", err)
	}
	return nil
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
			Address: p.GetConf().GetNeighborAddress(),
			State:   p.GetState().GetSessionState().String(),
		}

		if p.GetState().GetSessionState() == apipb.PeerState_SESSION_STATE_ESTABLISHED {
			ps.Established = true
			if p.GetTimers() != nil && p.GetTimers().GetState() != nil {
				ps.UpSince = p.GetTimers().GetState().GetUptime().GetSeconds()
			}
		}

		for _, af := range p.GetAfiSafis() {
			if af.GetConfig() != nil && af.GetConfig().GetFamily() != nil {
				if af.GetConfig().GetFamily().GetAfi() == apipb.Family_AFI_IP6 {
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
