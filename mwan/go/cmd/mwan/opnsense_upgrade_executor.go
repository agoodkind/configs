package main

import (
	"errors"
	"fmt"
	"log/slog"

	"goodkind.io/mwan/internal/opnsense"
	"goodkind.io/mwan/internal/opnsense/upgrade"
	"goodkind.io/mwan/internal/ops"
)

// upgradeExecutorFactory builds an upgrade.Executor from the parsed
// transport flags. It mirrors envFactory: ssh routes through the
// existing opsExecutorAdapter (QGA over Proxmox), grpc routes through
// the mwan-opnsense daemon's Exec RPC. Tests inject a fake dial
// function so the unit tests do not open a real unix socket.
type upgradeExecutorFactory struct {
	// dial returns the OPNsenseRPCClient bound to a target. Production
	// uses upgradeExecutorDialAdapter; tests inject a fake.
	dial func(target string) (upgrade.OPNsenseRPCClient, error)
}

// defaultUpgradeExecutorFactory returns the production factory wired
// to opnsense.Dial. The factory is dial-injected so the unit tests in
// opnsense_upgrade_test.go can drive the gRPC selection path without
// opening a real socket.
func defaultUpgradeExecutorFactory() upgradeExecutorFactory {
	return upgradeExecutorFactory{dial: upgradeExecutorDialAdapter}
}

// upgradeExecutorDialAdapter narrows *opnsense.Client.RPC() onto the
// upgrade.OPNsenseRPCClient interface. The validate package owns a
// parallel adapter (opnsenseDialAdapter in opnsense_env_transport.go)
// because it needs validate.OPNsenseRPCClient instead; the two
// interfaces have the same method set but distinct type identities so
// the cast must happen at the call site.
func upgradeExecutorDialAdapter(target string) (upgrade.OPNsenseRPCClient, error) {
	cli, err := opnsense.Dial(target)
	if err != nil {
		slog.Error("opnsense_upgrade_executor: dial mwan-opnsense gRPC failed",
			"target", target, "err", err.Error())
		return nil, fmt.Errorf("dial mwan-opnsense gRPC: %w", err)
	}
	return cli.RPC(), nil
}

// build returns an Executor matching the transport selection. SSH is
// the default and uses the QGA-backed adapter that has shipped since
// MWAN-152. gRPC opens the persistent virtio-serial channel to the
// mwan-opnsense daemon and dispatches each GuestExec through the
// daemon's Exec RPC. The grpc target must be supplied: an empty string
// is rejected at this layer so the operator gets a clear flag-error
// rather than a dial failure.
func (f upgradeExecutorFactory) build(
	cfg envTransportConfig, sshOps ops.SysOps,
) (upgrade.Executor, error) {
	switch cfg.Transport {
	case "", envTransportSSH:
		return opsExecutorAdapter{ops: sshOps}, nil
	case envTransportGRPC:
		return f.buildGRPCExecutor(cfg)
	default:
		err := errors.New("upgradeExecutorFactory: unknown transport")
		slog.Error("upgradeExecutorFactory: unknown transport",
			"transport", string(cfg.Transport),
			"err", err.Error())
		return nil, err
	}
}

func (f upgradeExecutorFactory) buildGRPCExecutor(
	cfg envTransportConfig,
) (*upgrade.GRPCExecutor, error) {
	if cfg.GRPCTarget == "" {
		err := errors.New("--env-transport=grpc requires --env-grpc-target")
		slog.Error("upgradeExecutorFactory: missing GRPCTarget for gRPC transport",
			"err", err.Error())
		return nil, err
	}
	if f.dial == nil {
		err := errors.New("upgradeExecutorFactory: dial function is nil")
		slog.Error("upgradeExecutorFactory: nil dial", "err", err.Error())
		return nil, err
	}
	rpc, err := f.dial(cfg.GRPCTarget)
	if err != nil {
		slog.Error("upgradeExecutorFactory: dial failed",
			"target", cfg.GRPCTarget, "err", err.Error())
		return nil, fmt.Errorf("upgradeExecutorFactory: dial %s: %w", cfg.GRPCTarget, err)
	}
	// Capture the dial closure so the executor can transparently
	// reconnect when the daemon's connection drops mid-flow. MWAN-178:
	// `qm rollback` resets QEMU and kills the virtio-serial channel,
	// so the post-rollback waitForGuest poll needs a fresh client. The
	// factory already validated the target string above, so the
	// closure can re-use it directly.
	target := cfg.GRPCTarget
	dial := f.dial
	redial := func() (upgrade.OPNsenseRPCClient, error) {
		return dial(target)
	}
	return &upgrade.GRPCExecutor{
		RPC:                rpc,
		ExecTimeoutSeconds: cfg.ExecTimeoutSeconds,
		Redial:             redial,
	}, nil
}
