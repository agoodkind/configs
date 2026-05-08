package main

import (
	"errors"
	"fmt"
	"log/slog"

	"goodkind.io/mwan/internal/opnsense"
	"goodkind.io/mwan/internal/opnsense/validate"
)

// envTransport names the supported --env-transport values for the
// opnsense-validate and opnsense-upgrade subcommands. The default is
// SSH so existing operator habits keep working; gRPC opts in to the
// MWAN-163 OOB path that survives a broken pf rule set.
type envTransport string

const (
	envTransportSSH  envTransport = "ssh"
	envTransportGRPC envTransport = "grpc"
)

// parseEnvTransport validates a raw flag value. An empty string maps
// to the SSH default so the flag stays optional.
func parseEnvTransport(raw string) (envTransport, error) {
	switch raw {
	case "", string(envTransportSSH):
		return envTransportSSH, nil
	case string(envTransportGRPC):
		return envTransportGRPC, nil
	default:
		return "", fmt.Errorf(
			"--env-transport must be ssh|grpc (got %q)", raw)
	}
}

// envTransportConfig is the small, transport-agnostic struct that the
// factory consumes. Both subcommand flag structs project into this.
type envTransportConfig struct {
	Transport           envTransport
	GRPCTarget          string
	OPNsenseSSHHost     string
	OPNsenseSSHJumpHost string
	ProxmoxSSHHost      string
	LANClientSSHHost    string
	OPNsenseAddr        string
}

// envFactory wraps the gRPC client constructor so tests can inject a
// fake. Production code uses opnsenseDialFunc which delegates to
// opnsense.Dial.
type envFactory struct {
	dial func(target string) (validate.OPNsenseRPCClient, error)
}

// defaultEnvFactory returns the production factory wired to
// opnsense.Dial.
func defaultEnvFactory() envFactory {
	return envFactory{dial: opnsenseDialAdapter}
}

// opnsenseDialAdapter narrows *opnsense.Client.RPC() to the
// validate.OPNsenseRPCClient interface so the factory can hold a
// closer alongside the typed RPC.
func opnsenseDialAdapter(target string) (validate.OPNsenseRPCClient, error) {
	cli, err := opnsense.Dial(target)
	if err != nil {
		slog.Error("opnsense_env_transport: dial mwan-opnsense gRPC failed",
			"target", target, "err", err.Error())
		return nil, fmt.Errorf("dial mwan-opnsense gRPC: %w", err)
	}
	return cli.RPC(), nil
}

// build constructs the validate.Env requested by cfg.Transport. The
// gRPC env requires GRPCTarget; the SSH env requires the SSH-host
// fields it actually uses, but those are validated lazily by the
// individual checks (matching the existing ExecEnv behaviour).
func (f envFactory) build(cfg envTransportConfig) (validate.Env, error) {
	switch cfg.Transport {
	case "", envTransportSSH:
		return buildSSHEnv(cfg), nil
	case envTransportGRPC:
		return f.buildGRPCEnv(cfg)
	default:
		err := errors.New("envFactory: unknown transport")
		slog.Error("envFactory: unknown transport",
			"transport", string(cfg.Transport),
			"err", err.Error())
		return nil, err
	}
}

func buildSSHEnv(cfg envTransportConfig) *validate.ExecEnv {
	return &validate.ExecEnv{
		OPNsenseSSHHost:     cfg.OPNsenseSSHHost,
		OPNsenseSSHJumpHost: cfg.OPNsenseSSHJumpHost,
		ProxmoxSSHHost:      cfg.ProxmoxSSHHost,
		LANClientSSHHost:    cfg.LANClientSSHHost,
		OPNsenseAddr:        cfg.OPNsenseAddr,
		HTTPClient:          nil,
		Clock:               nil,
	}
}

func (f envFactory) buildGRPCEnv(cfg envTransportConfig) (*validate.GRPCEnv, error) {
	if cfg.GRPCTarget == "" {
		err := errors.New("--env-transport=grpc requires --env-grpc-target")
		slog.Error("envFactory: missing GRPCTarget for gRPC transport",
			"err", err.Error())
		return nil, err
	}
	if f.dial == nil {
		err := errors.New("envFactory: dial function is nil")
		slog.Error("envFactory: nil dial", "err", err.Error())
		return nil, err
	}
	rpc, err := f.dial(cfg.GRPCTarget)
	if err != nil {
		slog.Error("envFactory: dial failed",
			"target", cfg.GRPCTarget, "err", err.Error())
		return nil, fmt.Errorf("envFactory: dial %s: %w", cfg.GRPCTarget, err)
	}
	return &validate.GRPCEnv{
		RPC:                rpc,
		Fallback:           buildSSHEnv(cfg),
		ExecTimeoutSeconds: 0,
		Clock:              nil,
	}, nil
}
