package bgp

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	apipb "github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/server"
)

// fakeBGPServer captures calls into the bgpServerAPI surface. Tests
// install one via Speaker.newServer to assert that the GR-related
// fields propagate into the GoBGP API requests built by the speaker.
type fakeBGPServer struct {
	mu              sync.Mutex
	startReq        *apipb.StartBgpRequest
	stopReq         *apipb.StopBgpRequest
	addPeerReqs     []*apipb.AddPeerRequest
	watchRegistered bool
}

func newFakeBGPServer() *fakeBGPServer {
	return &fakeBGPServer{
		mu:              sync.Mutex{},
		startReq:        nil,
		stopReq:         nil,
		addPeerReqs:     nil,
		watchRegistered: false,
	}
}

func (f *fakeBGPServer) Serve() {}

func (f *fakeBGPServer) StartBgp(_ context.Context, r *apipb.StartBgpRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startReq = r
	return nil
}

func (f *fakeBGPServer) StopBgp(_ context.Context, r *apipb.StopBgpRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopReq = r
	return nil
}

func (f *fakeBGPServer) AddPeer(_ context.Context, r *apipb.AddPeerRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addPeerReqs = append(f.addPeerReqs, r)
	return nil
}

func (f *fakeBGPServer) WatchEvent(_ context.Context, _ server.WatchEventMessageCallbacks, _ ...server.WatchOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchRegistered = true
	return nil
}

func (f *fakeBGPServer) ListPeer(_ context.Context, _ *apipb.ListPeerRequest, _ func(*apipb.Peer)) error {
	return nil
}

func (f *fakeBGPServer) AddPath(_ apiutil.AddPathRequest) ([]apiutil.AddPathResponse, error) {
	return nil, nil
}

func (f *fakeBGPServer) DeletePath(_ apiutil.DeletePathRequest) error {
	return nil
}

// discardLogger returns a logger whose output is discarded so tests do
// not pollute stderr. The level is irrelevant for the assertions.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newSpeakerWithFake returns a Speaker with the given Config wired to
// the supplied fakeBGPServer. The newServer hook bypasses the real
// GoBGP constructor so Start does not bind a TCP listener.
func newSpeakerWithFake(cfg Config, fake *fakeBGPServer) *Speaker {
	return &Speaker{
		cfg:        cfg,
		log:        discardLogger(),
		server:     nil,
		newServer:  func(_ *slog.Logger) bgpServerAPI { return fake },
		mu:         sync.Mutex{},
		announcing: false,
		started:    false,
	}
}

// baseGRConfig builds a minimal Config that drives the addPeer path
// through both v4 and v6 branches so the test can assert MpGracefulRestart
// on every AfiSafi entry.
func baseGRConfig(grEnabled bool) Config {
	return Config{
		Enabled:          true,
		ASN:              65001,
		RouterID:         "10.0.0.1",
		NextHopV6:        "",
		KeepaliveSeconds: 10,
		HoldSeconds:      30,
		ListenPort:       179,
		Neighbors:        []NeighborConfig{{Address: "10.0.0.2"}},
		NeighborsV6:      []NeighborConfig{{Address: "fd00::2"}},
		Announce: AnnounceConfig{
			IPv4: []string{"0.0.0.0/0"},
			IPv6: []string{"::/0"},
		},
		GracefulRestart: GracefulRestartConfig{
			Enabled:             grEnabled,
			RestartTime:         30,
			NotificationEnabled: true,
		},
	}
}

func TestStartPropagatesGracefulRestartToGlobal(t *testing.T) {
	t.Parallel()
	fake := newFakeBGPServer()
	cfg := baseGRConfig(true)
	s := newSpeakerWithFake(cfg, fake)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if fake.startReq == nil {
		t.Fatal("StartBgp was not called")
	}
	if fake.startReq.Global == nil {
		t.Fatal("StartBgp called with nil Global")
	}
	gr := fake.startReq.Global.GracefulRestart
	if gr == nil {
		t.Fatal("Global.GracefulRestart is nil; expected GR config to propagate")
	}
	if !gr.Enabled {
		t.Error("Global.GracefulRestart.Enabled = false; want true")
	}
	if gr.RestartTime != 30 {
		t.Errorf("Global.GracefulRestart.RestartTime = %d; want 30", gr.RestartTime)
	}
	if !gr.NotificationEnabled {
		t.Error("Global.GracefulRestart.NotificationEnabled = false; want true")
	}
}

func TestStartOmitsGracefulRestartWhenDisabled(t *testing.T) {
	t.Parallel()
	fake := newFakeBGPServer()
	cfg := baseGRConfig(false)
	s := newSpeakerWithFake(cfg, fake)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if fake.startReq == nil {
		t.Fatal("StartBgp was not called")
	}
	if fake.startReq.Global == nil {
		t.Fatal("StartBgp called with nil Global")
	}
	if fake.startReq.Global.GracefulRestart != nil {
		t.Errorf("Global.GracefulRestart = %+v; want nil when GR disabled", fake.startReq.Global.GracefulRestart)
	}
}

func TestAddPeerSetsGracefulRestartAndMpGracefulRestart(t *testing.T) {
	t.Parallel()
	fake := newFakeBGPServer()
	cfg := baseGRConfig(true)
	s := newSpeakerWithFake(cfg, fake)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if len(fake.addPeerReqs) != 2 {
		t.Fatalf("AddPeer call count = %d; want 2 (one v4, one v6)", len(fake.addPeerReqs))
	}
	for i, req := range fake.addPeerReqs {
		if req.Peer == nil {
			t.Fatalf("AddPeer[%d].Peer is nil", i)
		}
		if req.Peer.GracefulRestart == nil {
			t.Errorf("AddPeer[%d].Peer.GracefulRestart is nil; want set when GR enabled", i)
			continue
		}
		if !req.Peer.GracefulRestart.Enabled {
			t.Errorf("AddPeer[%d].Peer.GracefulRestart.Enabled = false; want true", i)
		}
		if req.Peer.GracefulRestart.RestartTime != 30 {
			t.Errorf("AddPeer[%d].Peer.GracefulRestart.RestartTime = %d; want 30", i, req.Peer.GracefulRestart.RestartTime)
		}
		if len(req.Peer.AfiSafis) != 1 {
			t.Fatalf("AddPeer[%d].Peer.AfiSafis len = %d; want 1", i, len(req.Peer.AfiSafis))
		}
		af := req.Peer.AfiSafis[0]
		if af.MpGracefulRestart == nil || af.MpGracefulRestart.Config == nil {
			t.Errorf("AddPeer[%d] AfiSafi[0].MpGracefulRestart.Config is nil; want set when GR enabled", i)
			continue
		}
		if !af.MpGracefulRestart.Config.Enabled {
			t.Errorf("AddPeer[%d] AfiSafi[0].MpGracefulRestart.Config.Enabled = false; want true", i)
		}
	}
}

func TestAddPeerOmitsGracefulRestartWhenDisabled(t *testing.T) {
	t.Parallel()
	fake := newFakeBGPServer()
	cfg := baseGRConfig(false)
	s := newSpeakerWithFake(cfg, fake)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	for i, req := range fake.addPeerReqs {
		if req.Peer.GracefulRestart != nil {
			t.Errorf("AddPeer[%d].Peer.GracefulRestart = %+v; want nil when GR disabled", i, req.Peer.GracefulRestart)
		}
		for j, af := range req.Peer.AfiSafis {
			if af.MpGracefulRestart != nil {
				t.Errorf("AddPeer[%d] AfiSafi[%d].MpGracefulRestart = %+v; want nil when GR disabled", i, j, af.MpGracefulRestart)
			}
		}
	}
}

func TestStopPassesAllowGracefulRestartWhenEnabled(t *testing.T) {
	t.Parallel()
	fake := newFakeBGPServer()
	cfg := baseGRConfig(true)
	s := newSpeakerWithFake(cfg, fake)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if fake.stopReq == nil {
		t.Fatal("StopBgp was not called")
	}
	if !fake.stopReq.AllowGracefulRestart {
		t.Error("StopBgpRequest.AllowGracefulRestart = false; want true when GR enabled")
	}
}

func TestStopOmitsAllowGracefulRestartWhenDisabled(t *testing.T) {
	t.Parallel()
	fake := newFakeBGPServer()
	cfg := baseGRConfig(false)
	s := newSpeakerWithFake(cfg, fake)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if fake.stopReq == nil {
		t.Fatal("StopBgp was not called")
	}
	if fake.stopReq.AllowGracefulRestart {
		t.Error("StopBgpRequest.AllowGracefulRestart = true; want false when GR disabled")
	}
}
