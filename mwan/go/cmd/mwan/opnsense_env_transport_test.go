package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsense/validate"
)

func TestParseEnvTransport(t *testing.T) {
	cases := []struct {
		raw     string
		want    envTransport
		wantErr bool
	}{
		{"", envTransportSSH, false},
		{"ssh", envTransportSSH, false},
		{"grpc", envTransportGRPC, false},
		{"qga", "", true},
	}
	for _, c := range cases {
		got, err := parseEnvTransport(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseEnvTransport(%q) want err, got %q", c.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseEnvTransport(%q) unexpected err: %v", c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseEnvTransport(%q)=%q want %q", c.raw, got, c.want)
		}
	}
}

func TestEnvFactoryBuildSSH(t *testing.T) {
	got, err := defaultEnvFactory().build(envTransportConfig{
		Transport:           envTransportSSH,
		GRPCTarget:          "",
		OPNsenseSSHHost:     "h",
		OPNsenseSSHJumpHost: "",
		ProxmoxSSHHost:      "",
		LANClientSSHHost:    "",
		OPNsenseAddr:        "",
	})
	if err != nil {
		t.Fatalf("build ssh err: %v", err)
	}
	if _, ok := got.(*validate.ExecEnv); !ok {
		t.Fatalf("ssh transport produced %T, want *validate.ExecEnv", got)
	}
}

func TestEnvFactoryBuildSSHEmptyTransportDefaultsToSSH(t *testing.T) {
	got, err := defaultEnvFactory().build(envTransportConfig{
		Transport:           "",
		GRPCTarget:          "",
		OPNsenseSSHHost:     "h",
		OPNsenseSSHJumpHost: "",
		ProxmoxSSHHost:      "",
		LANClientSSHHost:    "",
		OPNsenseAddr:        "",
	})
	if err != nil {
		t.Fatalf("build empty transport err: %v", err)
	}
	if _, ok := got.(*validate.ExecEnv); !ok {
		t.Fatalf("empty transport produced %T, want *validate.ExecEnv", got)
	}
}

type fakeRPCForFactory struct{}

func (fakeRPCForFactory) Exec(_ context.Context, _ *mwanv1.ExecRequest) (*mwanv1.ExecResponse, error) {
	return &mwanv1.ExecResponse{
		Stdout:          nil,
		Stderr:          nil,
		ExitCode:        0,
		DurationMs:      0,
		StdoutTruncated: false,
		StderrTruncated: false,
		TimedOut:        false,
	}, nil
}

func TestEnvFactoryBuildGRPC(t *testing.T) {
	dialed := ""
	factory := envFactory{
		dial: func(target string) (validate.OPNsenseRPCClient, error) {
			dialed = target
			return fakeRPCForFactory{}, nil
		},
	}
	got, err := factory.build(envTransportConfig{
		Transport:           envTransportGRPC,
		GRPCTarget:          "unix:///tmp/test.sock",
		OPNsenseSSHHost:     "h",
		OPNsenseSSHJumpHost: "",
		ProxmoxSSHHost:      "vault",
		LANClientSSHHost:    "lan",
		OPNsenseAddr:        "",
	})
	if err != nil {
		t.Fatalf("build grpc err: %v", err)
	}
	grpcEnv, ok := got.(*validate.GRPCEnv)
	if !ok {
		t.Fatalf("grpc transport produced %T, want *validate.GRPCEnv", got)
	}
	if grpcEnv.RPC == nil {
		t.Fatalf("grpcEnv.RPC nil")
	}
	if grpcEnv.Fallback == nil {
		t.Fatalf("grpcEnv.Fallback nil")
	}
	if grpcEnv.Fallback.ProxmoxSSHHost != "vault" {
		t.Fatalf("Fallback.ProxmoxSSHHost=%q want vault", grpcEnv.Fallback.ProxmoxSSHHost)
	}
	if dialed != "unix:///tmp/test.sock" {
		t.Fatalf("dialed=%q want unix:///tmp/test.sock", dialed)
	}
}

func TestEnvFactoryGRPCRequiresTarget(t *testing.T) {
	factory := envFactory{
		dial: func(_ string) (validate.OPNsenseRPCClient, error) {
			t.Fatalf("dial must not be called")
			return nil, nil
		},
	}
	_, err := factory.build(envTransportConfig{
		Transport:           envTransportGRPC,
		GRPCTarget:          "",
		OPNsenseSSHHost:     "",
		OPNsenseSSHJumpHost: "",
		ProxmoxSSHHost:      "",
		LANClientSSHHost:    "",
		OPNsenseAddr:        "",
	})
	if err == nil {
		t.Fatalf("want error when grpc transport has no target")
	}
	if !strings.Contains(err.Error(), "env-grpc-target") {
		t.Fatalf("err=%v missing env-grpc-target hint", err)
	}
}

func TestEnvFactoryGRPCDialErrorPropagates(t *testing.T) {
	factory := envFactory{
		dial: func(_ string) (validate.OPNsenseRPCClient, error) {
			return nil, errors.New("dial refused")
		},
	}
	_, err := factory.build(envTransportConfig{
		Transport:           envTransportGRPC,
		GRPCTarget:          "unix:///nope",
		OPNsenseSSHHost:     "",
		OPNsenseSSHJumpHost: "",
		ProxmoxSSHHost:      "",
		LANClientSSHHost:    "",
		OPNsenseAddr:        "",
	})
	if err == nil {
		t.Fatalf("want dial error to propagate")
	}
	if !strings.Contains(err.Error(), "dial refused") {
		t.Fatalf("err=%v missing dial refused", err)
	}
}
