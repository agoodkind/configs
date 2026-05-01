//go:build linux

// Package policyrules implements the ifmgr policy-rules module: keeps a
// configured list of `ip rule` entries present in the kernel. Each rule
// is an (priority, family, selector, table) tuple. Foreign rules at
// unrelated priorities are not touched.
//
// Registers as "policy_rules". Selected by the vault-oob role today;
// reusable for any role that needs static policy routing.
package policyrules

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

// Module owns the policy-rules state.
type Module struct {
	cfg Config
	env *ifmgr.Env
	log *slog.Logger
}

// Config is the parsed [ifmgr.modules.policy_rules] sub-config. Note the
// table is named "rule" in TOML to match the [[ifmgr.modules.policy_rules.rule]]
// array-of-tables idiom; here we accept the parsed list of rules.
type Config struct {
	Rules []netif.DesiredRule
}

func (Config) ModuleConfigName() string { return "policy_rules" }

// Name implements ifmgr.Module.
func (m *Module) Name() string { return "policy_rules" }

// Init implements ifmgr.Module. Sanity-checks each rule.
func (m *Module) Init(_ context.Context, env *ifmgr.Env) error {
	m.env = env
	m.log = env.Log.With("module", "policy_rules")
	m.log.Info("policy_rules: Init", "rule_count", len(m.cfg.Rules))
	for i, r := range m.cfg.Rules {
		if r.Priority <= 0 {
			return fmt.Errorf("policy_rules[%d]: priority must be > 0", i)
		}
		if r.TableID <= 0 {
			return fmt.Errorf("policy_rules[%d]: table_id must be > 0 (rule prio=%d)", i, r.Priority)
		}
	}
	return nil
}

// Reconcile implements ifmgr.Module.
func (m *Module) Reconcile(ctx context.Context, log *slog.Logger) error {
	return netif.ReconcileRules(ctx, log, m.cfg.Rules)
}

// OnKernelEvent implements ifmgr.Module. Rule events are not subscribed
// today; rely on the periodic Reconcile to catch external mutations.
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
	c := Config{}
	if cfg != nil {
		typedConfig, ok := cfg.(Config)
		if !ok {
			return nil, fmt.Errorf("policy_rules: invalid config type %T", cfg)
		}
		c = typedConfig
	}
	return &Module{cfg: c}, nil
}

func init() { ifmgr.Register("policy_rules", New) }
