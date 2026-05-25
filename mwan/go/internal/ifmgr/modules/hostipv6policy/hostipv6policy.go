//go:build linux

// Package hostipv6policy keeps the suburban hypervisor's host-side IPv6
// RA policy aligned with the intended bridge roles. Proxmox still owns
// bridge existence and shape; this module only reconciles live kernel
// sysctls and removes stale RA-learned defaults from denied interfaces.
package hostipv6policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/mdlayher/ndp"
	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

const (
	defaultMissingIfaceGracePeriod = 2 * time.Minute
	raSolicitTimeout               = 5 * time.Second
)

// InterfacePolicy is one host interface's desired IPv6 RA behaviour.
type InterfacePolicy struct {
	Name             string
	AcceptRA         int
	AutoConf         bool
	AcceptRADefRtr   bool
	SolicitRA        bool
	CleanupRADefault bool
}

// Config is the parsed [ifmgr.modules.host_ipv6_policy] sub-config.
type Config struct {
	MissingIfaceGracePeriod time.Duration
	Policies                []InterfacePolicy
}

func (Config) ModuleConfigName() string { return "host_ipv6_policy" }

type routerSoliciter interface {
	SolicitRA(ctx context.Context, timeout time.Duration) (*ndp.RouterAdvertisement, error)
	Close() error
}

// Module owns the host-side IPv6 RA policy.
type Module struct {
	cfg Config
	env *ifmgr.Env
	log *slog.Logger

	mu           sync.Mutex
	missingSince map[string]time.Time

	now                  func() time.Time
	findMainRADefault    func(context.Context, string) (*netif.CurrentRoute, error)
	deleteMainRADefaults func(context.Context, *slog.Logger, string) (int, error)
	newRAClient          func(string, *slog.Logger) (routerSoliciter, error)
}

// Name implements ifmgr.Module.
func (m *Module) Name() string { return "host_ipv6_policy" }

// Init implements ifmgr.Module.
func (m *Module) Init(_ context.Context, env *ifmgr.Env) error {
	m.env = env
	m.log = env.Log.With("module", "host_ipv6_policy")
	m.log.Info("host_ipv6_policy: Init",
		"policy_count", len(m.cfg.Policies),
		"missing_iface_grace_period", m.cfg.MissingIfaceGracePeriod.String(),
	)
	if m.cfg.MissingIfaceGracePeriod <= 0 {
		return fmt.Errorf("host_ipv6_policy: missing_iface_grace_period must be > 0")
	}
	if len(m.cfg.Policies) == 0 {
		return fmt.Errorf("host_ipv6_policy: at least one interface policy is required")
	}
	seenIfaces := make(map[string]bool, len(m.cfg.Policies))
	for i, policy := range m.cfg.Policies {
		if policy.Name == "" {
			return fmt.Errorf("host_ipv6_policy[%d]: name is required", i)
		}
		if policy.AcceptRA < 0 || policy.AcceptRA > 2 {
			return fmt.Errorf("host_ipv6_policy[%d]: accept_ra must be 0, 1, or 2", i)
		}
		if seenIfaces[policy.Name] {
			return fmt.Errorf("host_ipv6_policy[%d]: duplicate iface %q", i, policy.Name)
		}
		seenIfaces[policy.Name] = true
	}
	return nil
}

// Reconcile implements ifmgr.Module.
func (m *Module) Reconcile(ctx context.Context, log *slog.Logger) error {
	var reconcileErr error
	for _, policy := range m.cfg.Policies {
		policyLog := log.With("policy_iface", policy.Name)
		if err := m.reconcilePolicy(ctx, policyLog, policy); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
		}
	}
	return reconcileErr
}

func (m *Module) reconcilePolicy(
	ctx context.Context,
	log *slog.Logger,
	policy InterfacePolicy,
) error {
	missingIface, err := m.reconcilePolicySysctls(ctx, log, policy)
	if err != nil {
		if missingIface {
			return m.handleMissingIface(log, policy.Name, err)
		}
		return err
	}
	m.clearMissingIface(policy.Name)

	if policy.CleanupRADefault {
		return m.cleanupDeniedRADefault(ctx, log, policy.Name)
	}
	if policy.SolicitRA {
		return m.solicitAllowedIfaceRA(ctx, log, policy.Name)
	}
	return nil
}

func (m *Module) reconcilePolicySysctls(
	ctx context.Context,
	log *slog.Logger,
	policy InterfacePolicy,
) (bool, error) {
	sysctls := []struct {
		key  string
		want string
	}{
		{key: acceptRAKey(policy.Name), want: fmt.Sprintf("%d", policy.AcceptRA)},
		{key: autoconfKey(policy.Name), want: boolToSysctl(policy.AutoConf)},
		{key: acceptRADefRtrKey(policy.Name), want: boolToSysctl(policy.AcceptRADefRtr)},
	}
	for _, sysctl := range sysctls {
		missingIface, err := m.reconcileSysctl(ctx, log, sysctl.key, sysctl.want)
		if err != nil {
			return missingIface, err
		}
	}
	return false, nil
}

func (m *Module) reconcileSysctl(
	ctx context.Context,
	log *slog.Logger,
	key string,
	want string,
) (bool, error) {
	currentValue, err := m.env.Sysctl.Get(ctx, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, err
		}
		return false, err
	}
	if currentValue == want {
		return false, nil
	}
	log.Info("host_ipv6_policy: updating sysctl",
		"key", key,
		"current", currentValue,
		"want", want,
	)
	if err := m.env.Sysctl.Set(ctx, key, want); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, err
		}
		return false, err
	}
	return false, nil
}

func (m *Module) cleanupDeniedRADefault(
	ctx context.Context,
	log *slog.Logger,
	iface string,
) error {
	currentRoute, err := m.findMainRADefault(ctx, iface)
	if err != nil {
		return err
	}
	if currentRoute == nil {
		return nil
	}
	log.Info("host_ipv6_policy: removing denied RA default",
		"via", currentRoute.Via,
		"dev", currentRoute.Dev,
	)
	deletedCount, err := m.deleteMainRADefaults(ctx, log, iface)
	if err != nil {
		return err
	}
	if deletedCount > 0 {
		log.Info("host_ipv6_policy: removed denied RA defaults",
			"count", deletedCount,
		)
	}
	return nil
}

func (m *Module) solicitAllowedIfaceRA(
	ctx context.Context,
	log *slog.Logger,
	iface string,
) error {
	currentRoute, err := m.findMainRADefault(ctx, iface)
	if err != nil {
		return err
	}
	if currentRoute != nil {
		return nil
	}
	client, err := m.newRAClient(iface, log)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			log.Debug("host_ipv6_policy: RA client close failed", "err", closeErr)
		}
	}()
	log.Info("host_ipv6_policy: soliciting RA on allowed iface")
	if _, err := client.SolicitRA(ctx, raSolicitTimeout); err != nil {
		log.Warn("host_ipv6_policy: RA solicit did not yield a usable advertisement", "err", err)
		return nil
	}
	currentRoute, err = m.findMainRADefault(ctx, iface)
	if err != nil {
		return err
	}
	if currentRoute != nil {
		log.Info("host_ipv6_policy: RA default learned",
			"via", currentRoute.Via,
			"dev", currentRoute.Dev,
		)
	} else {
		log.Warn("host_ipv6_policy: RA solicit completed without a main-table default")
	}
	return nil
}

func (m *Module) handleMissingIface(log *slog.Logger, iface string, cause error) error {
	now := m.now()
	m.mu.Lock()
	firstSeenAt, exists := m.missingSince[iface]
	if !exists {
		firstSeenAt = now
		m.missingSince[iface] = now
	}
	m.mu.Unlock()

	missingDuration := now.Sub(firstSeenAt)
	if missingDuration < m.cfg.MissingIfaceGracePeriod {
		log.Info("host_ipv6_policy: waiting for iface to appear",
			"iface", iface,
			"age", missingDuration.String(),
			"grace_period", m.cfg.MissingIfaceGracePeriod.String(),
		)
		return nil
	}
	return fmt.Errorf(
		"host_ipv6_policy: iface %q still missing after %s: %w",
		iface,
		missingDuration.Truncate(time.Second).String(),
		cause,
	)
}

func (m *Module) clearMissingIface(iface string) {
	m.mu.Lock()
	delete(m.missingSince, iface)
	m.mu.Unlock()
}

// OnKernelEvent implements ifmgr.Module.
func (m *Module) OnKernelEvent(_ context.Context, _ *slog.Logger, _ netif.Event) error {
	return nil
}

// OnDHCPLease implements ifmgr.Module.
func (m *Module) OnDHCPLease(_ context.Context, _ *slog.Logger, _ netif.LeaseInfo) error {
	return nil
}

// EvaluateAlerts implements ifmgr.Module.
func (m *Module) EvaluateAlerts(_ context.Context, _ *slog.Logger, _ time.Time) {
}

// New is the Constructor.
func New(cfg ifmgr.ModuleConfig) (ifmgr.Module, error) {
	parsedConfig := Config{
		MissingIfaceGracePeriod: defaultMissingIfaceGracePeriod,
	}
	if cfg != nil {
		typedConfig, ok := cfg.(Config)
		if !ok {
			return nil, fmt.Errorf("host_ipv6_policy: invalid config type %T", cfg)
		}
		parsedConfig = typedConfig
		if parsedConfig.MissingIfaceGracePeriod == 0 {
			parsedConfig.MissingIfaceGracePeriod = defaultMissingIfaceGracePeriod
		}
	}
	return &Module{
		cfg:                  parsedConfig,
		missingSince:         make(map[string]time.Time),
		now:                  time.Now,
		findMainRADefault:    netif.FindMainRADefault,
		deleteMainRADefaults: netif.DeleteMainRADefaults,
		newRAClient: func(iface string, log *slog.Logger) (routerSoliciter, error) {
			return netif.NewRAClient(iface, log)
		},
	}, nil
}

func acceptRAKey(iface string) string {
	return fmt.Sprintf("net.ipv6.conf.%s.accept_ra", iface)
}

func autoconfKey(iface string) string {
	return fmt.Sprintf("net.ipv6.conf.%s.autoconf", iface)
}

func acceptRADefRtrKey(iface string) string {
	return fmt.Sprintf("net.ipv6.conf.%s.accept_ra_defrtr", iface)
}

func boolToSysctl(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

func init() { ifmgr.Register("host_ipv6_policy", New) }
