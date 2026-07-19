//go:build linux

// Package wanroutes ports the MWAN update-routes policy-routing inventory into
// an ifmgr module.
package wanroutes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"golang.org/x/sys/unix"
	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

const (
	moduleName          = "wan.routes"
	familyV4            = "inet"
	familyV6            = "inet6"
	fallbackPriority    = 50
	mainInternalMetric  = 1024
	wanNameATT          = "att"
	wanNameWebpass      = "webpass"
	wanNameMonkeybrains = "monkeybrains"
)

// Config is the parsed [ifmgr.modules.wan.routes] runtime config.
type Config struct {
	InternalIface   string
	OpnsenseWanLL   string
	OpnsenseEdgeV6  string
	InternalPrefix  string
	InternalNetV4   string
	HealthStateFile string
	ShadowMode      bool
	WANs            []WAN
}

// ModuleConfigName returns the registry key for this module's config block.
func (Config) ModuleConfigName() string { return moduleName }

// WAN is one configured uplink and its owned policy-routing slots. The
// embedded ifmgr.WANRef carries the shared per-WAN identity (Name, Iface); the
// remaining fields are the wan.routes-specific per-WAN routing data.
type WAN struct {
	ifmgr.WANRef
	TableID    int
	FwMark     uint32
	FwMarkPrio int
	FromPrio   int
	NptPrefix  string
	// V4Source is the WAN's static IPv4 link address. When set, traffic the box
	// sources from that address is pinned to this WAN's table via a v4 source
	// rule at FromPrio, the IPv4 twin of the NptPrefix v6 source rule. Only
	// static-link WANs (Webpass) set it; dynamic-link WANs (AT&T, Monkeybrains)
	// leave it empty and get no v4 source rule.
	V4Source string
}

type gatewaySet struct {
	V4 string
	V6 string
}

type gateways map[string]gatewaySet

type ruleSlot struct {
	family   string
	priority int
}

// Module owns the WAN policy-routing rules and routes.
type Module struct {
	ifmgr.BaseModule

	cfg Config
}

// Init implements ifmgr.Module.
func (m *Module) Init(ctx context.Context, env *ifmgr.Env) error {
	log := m.InitBase(env, "module", moduleName)
	log.InfoContext(
		ctx, "wan.routes: Init",
		"wan_count", len(m.cfg.WANs),
		"health_state_file", m.cfg.HealthStateFile,
		"shadow_mode", m.cfg.ShadowMode,
	)

	if len(m.cfg.WANs) == 0 {
		log.WarnContext(ctx, "wan.routes: missing WAN config; disabling module")
		return fmt.Errorf("%w: wan.routes: no [ifmgr.modules.wan.routes] section", ifmgr.ErrModuleDisabled)
	}
	if err := validateConfig(m.cfg); err != nil {
		log.WarnContext(ctx, "wan.routes: validateConfig failed", "err", err)
		return err
	}

	ifmgr.StartIfaceMonitors(ctx, log, moduleName, watchedIfaces(m.cfg), m.onMonitorEvent)
	return nil
}

// Reconcile implements ifmgr.Module.
func (m *Module) Reconcile(ctx context.Context, log *slog.Logger) error {
	m.Lock()
	defer m.Unlock()

	log = log.With("op", "reconcile")
	log.DebugContext(ctx, "wan.routes: Reconcile entry")

	currentGateways, err := discoverGateways(m.cfg)
	if err != nil {
		log.WarnContext(ctx, "wan.routes: discoverGateways failed", "err", err)
		return err
	}
	health, err := netif.ReadHealthState(m.cfg.HealthStateFile)
	if err != nil {
		log.WarnContext(ctx, "wan.routes: ReadHealthState failed", "err", err)
		return fmt.Errorf("read health state %q: %w", m.cfg.HealthStateFile, err)
	}
	rules, routes := desiredState(currentGateways, health, m.cfg)

	if m.cfg.ShadowMode {
		logShadowOps(log, m.cfg, rules, routes)
		return nil
	}

	var reconcileErr error
	for _, route := range routes {
		if route.Dest == "default" {
			if err := netif.ReconcileTableDefault(ctx, log, route); err != nil {
				reconcileErr = errors.Join(reconcileErr, fmt.Errorf(
					"reconcile default route table=%d family=%s: %w",
					route.TableID,
					route.Family,
					err,
				))
			}
			continue
		}
		if err := netif.ReconcileTableRoute(ctx, log, route); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf(
				"reconcile route table=%d family=%s dest=%s: %w",
				route.TableID,
				route.Family,
				route.Dest,
				err,
			))
		}
	}
	if err := netif.ReconcileRules(ctx, log, rules); err != nil {
		reconcileErr = errors.Join(reconcileErr, fmt.Errorf("reconcile rules: %w", err))
	}
	if err := removeDisabledRuleSlots(ctx, log, m.cfg, rules); err != nil {
		reconcileErr = errors.Join(reconcileErr, err)
	}
	return reconcileErr
}

func (m *Module) onMonitorEvent(ctx context.Context, log *slog.Logger, event netif.Event) {
	if !isDefaultRouteEvent(event) {
		return
	}
	eventLog := log.With(
		"kind", event.Kind.String(),
		"family", event.Family,
		"via", event.Via,
	)
	eventLog.DebugContext(ctx, "wan.routes: default route event, reconciling")
	if err := m.Reconcile(ctx, eventLog); err != nil {
		eventLog.WarnContext(ctx, "wan.routes: reconcile after route event failed", "err", err)
	}
}

func desiredState(
	currentGateways gateways,
	health netif.HealthStates,
	cfg Config,
) ([]netif.DesiredRule, []netif.RouteSpec) {
	rules := make([]netif.DesiredRule, 0, len(cfg.WANs)*3+2)
	routes := make([]netif.RouteSpec, 0, len(cfg.WANs)*5+1)

	for _, wan := range cfg.WANs {
		wanGateways := currentGateways[wan.Name]
		routes = appendWANDefaultRoutes(routes, wan, wanGateways)
		routes = appendWANInternalRoutes(routes, cfg, wan.TableID)

		rules = appendWANRules(rules, wan, wanGateways, health)
	}

	routes = appendMainInternalRoute(routes, cfg)

	monkeybrains := findWAN(cfg, wanNameMonkeybrains)
	if monkeybrains != nil && fallbackEnabled(health) {
		rules = append(
			rules,
			netif.DesiredRule{
				Family:   familyV4,
				Priority: fallbackPriority,
				From:     "",
				Mark:     0,
				IifName:  cfg.InternalIface,
				UIDRange: "",
				Table:    "",
				TableID:  monkeybrains.TableID,
			},
			netif.DesiredRule{
				Family:   familyV6,
				Priority: fallbackPriority,
				From:     "",
				Mark:     0,
				IifName:  cfg.InternalIface,
				UIDRange: "",
				Table:    "",
				TableID:  monkeybrains.TableID,
			},
		)
	}

	return rules, routes
}

func appendWANDefaultRoutes(routes []netif.RouteSpec, wan WAN, gateways gatewaySet) []netif.RouteSpec {
	if gateways.V4 != "" {
		routes = append(routes, netif.RouteSpec{
			Family:  familyV4,
			Dest:    "default",
			Via:     gateways.V4,
			Dev:     wan.Iface,
			TableID: wan.TableID,
			Metric:  0,
		})
	}
	if gateways.V6 != "" {
		routes = append(routes, netif.RouteSpec{
			Family:  familyV6,
			Dest:    "default",
			Via:     gateways.V6,
			Dev:     wan.Iface,
			TableID: wan.TableID,
			Metric:  0,
		})
	}
	return routes
}

func appendWANRules(
	rules []netif.DesiredRule,
	wan WAN,
	gateways gatewaySet,
	health netif.HealthStates,
) []netif.DesiredRule {
	if wanEnabled(gateways.V4, health.State(wan.Name)) {
		rules = append(rules, netif.DesiredRule{
			Family:   familyV4,
			Priority: wan.FwMarkPrio,
			From:     "",
			Mark:     wan.FwMark,
			IifName:  "",
			UIDRange: "",
			Table:    "",
			TableID:  wan.TableID,
		})
		if wan.V4Source != "" {
			rules = append(rules, netif.DesiredRule{
				Family:   familyV4,
				Priority: wan.FromPrio,
				From:     wan.V4Source,
				Mark:     0,
				IifName:  "",
				UIDRange: "",
				Table:    "",
				TableID:  wan.TableID,
			})
		}
	}
	if wanEnabled(gateways.V6, health.State(wan.Name)) {
		rules = append(rules, netif.DesiredRule{
			Family:   familyV6,
			Priority: wan.FwMarkPrio,
			From:     "",
			Mark:     wan.FwMark,
			IifName:  "",
			UIDRange: "",
			Table:    "",
			TableID:  wan.TableID,
		})
		if wan.NptPrefix != "" {
			rules = append(rules, netif.DesiredRule{
				Family:   familyV6,
				Priority: wan.FromPrio,
				From:     wan.NptPrefix,
				Mark:     0,
				IifName:  "",
				UIDRange: "",
				Table:    "",
				TableID:  wan.TableID,
			})
		}
	}
	return rules
}

func appendWANInternalRoutes(
	routes []netif.RouteSpec,
	cfg Config,
	tableID int,
) []netif.RouteSpec {
	routes = append(
		routes,
		netif.RouteSpec{
			Family:  familyV4,
			Dest:    cfg.InternalNetV4,
			Via:     "",
			Dev:     cfg.InternalIface,
			TableID: tableID,
			Metric:  0,
		},
		netif.RouteSpec{
			Family:  familyV6,
			Dest:    withPrefix(cfg.OpnsenseEdgeV6, "128"),
			Via:     "",
			Dev:     cfg.InternalIface,
			TableID: tableID,
			Metric:  0,
		},
		netif.RouteSpec{
			Family:  familyV6,
			Dest:    cfg.InternalPrefix,
			Via:     cfg.OpnsenseWanLL,
			Dev:     cfg.InternalIface,
			TableID: tableID,
			Metric:  0,
		},
	)
	return routes
}

func appendMainInternalRoute(routes []netif.RouteSpec, cfg Config) []netif.RouteSpec {
	routes = append(routes, netif.RouteSpec{
		Family:  familyV6,
		Dest:    cfg.InternalPrefix,
		Via:     cfg.OpnsenseWanLL,
		Dev:     cfg.InternalIface,
		TableID: unix.RT_TABLE_MAIN,
		Metric:  mainInternalMetric,
	})
	return routes
}

func discoverGateways(cfg Config) (gateways, error) {
	currentGateways := make(gateways, len(cfg.WANs))
	var gatewayErr error
	for _, wan := range cfg.WANs {
		wanGateways := gatewaySet{V4: "", V6: ""}
		gatewayV4, err := netif.IfaceDefaultGateway(familyV4, wan.Iface)
		if err != nil {
			gatewayErr = errors.Join(gatewayErr, fmt.Errorf(
				"%s %s default gateway: %w",
				wan.Name,
				familyV4,
				err,
			))
		}
		wanGateways.V4 = gatewayV4

		gatewayV6, err := netif.IfaceDefaultGateway(familyV6, wan.Iface)
		if err != nil {
			gatewayErr = errors.Join(gatewayErr, fmt.Errorf(
				"%s %s default gateway: %w",
				wan.Name,
				familyV6,
				err,
			))
		}
		wanGateways.V6 = gatewayV6
		currentGateways[wan.Name] = wanGateways
	}
	if gatewayErr != nil {
		return nil, gatewayErr
	}
	return currentGateways, nil
}

func removeDisabledRuleSlots(
	ctx context.Context,
	log *slog.Logger,
	cfg Config,
	rules []netif.DesiredRule,
) error {
	desiredSlots := desiredRuleSlots(rules)
	var removeErr error
	for _, slot := range ownedRuleSlots(cfg) {
		if desiredSlots[slot] {
			continue
		}
		if err := netif.RemoveRuleAtPriority(ctx, log, slot.family, slot.priority); err != nil {
			removeErr = errors.Join(removeErr, fmt.Errorf(
				"remove disabled rule family=%s priority=%d: %w",
				slot.family,
				slot.priority,
				err,
			))
		}
	}
	return removeErr
}

func logShadowOps(
	log *slog.Logger,
	cfg Config,
	rules []netif.DesiredRule,
	routes []netif.RouteSpec,
) {
	for _, route := range routes {
		log.Debug("wan.routes: shadow reconcile route", "route", route)
	}
	for _, rule := range rules {
		log.Debug("wan.routes: shadow reconcile rule", "rule", rule)
	}
	desiredSlots := desiredRuleSlots(rules)
	for _, slot := range ownedRuleSlots(cfg) {
		if desiredSlots[slot] {
			continue
		}
		log.Debug(
			"wan.routes: shadow remove disabled rule",
			"family", slot.family,
			"priority", slot.priority,
		)
	}
}

func desiredRuleSlots(rules []netif.DesiredRule) map[ruleSlot]bool {
	slots := make(map[ruleSlot]bool, len(rules))
	for _, rule := range rules {
		slots[ruleSlot{family: rule.Family, priority: rule.Priority}] = true
	}
	return slots
}

func ownedRuleSlots(cfg Config) []ruleSlot {
	seenSlots := make(map[ruleSlot]bool, len(cfg.WANs)*4+2)
	slots := make([]ruleSlot, 0, len(cfg.WANs)*4+2)
	appendSlot := func(slot ruleSlot) {
		if seenSlots[slot] {
			return
		}
		seenSlots[slot] = true
		slots = append(slots, slot)
	}
	appendSlot(ruleSlot{family: familyV4, priority: fallbackPriority})
	appendSlot(ruleSlot{family: familyV6, priority: fallbackPriority})
	for _, wan := range cfg.WANs {
		appendSlot(ruleSlot{family: familyV4, priority: wan.FwMarkPrio})
		appendSlot(ruleSlot{family: familyV6, priority: wan.FwMarkPrio})
		appendSlot(ruleSlot{family: familyV4, priority: wan.FromPrio})
		appendSlot(ruleSlot{family: familyV6, priority: wan.FromPrio})
	}
	return slots
}

func watchedIfaces(cfg Config) []string {
	seenIfaces := map[string]bool{}
	ifaces := make([]string, 0, len(cfg.WANs)+1)
	appendIface := func(iface string) {
		if iface == "" || seenIfaces[iface] {
			return
		}
		seenIfaces[iface] = true
		ifaces = append(ifaces, iface)
	}
	appendIface(cfg.InternalIface)
	for _, wan := range cfg.WANs {
		appendIface(wan.Iface)
	}
	return ifaces
}

func validateConfig(cfg Config) error {
	if cfg.InternalIface == "" {
		slog.Warn("wan.routes: missing internal_iface")
		return fmt.Errorf("wan.routes: internal_iface is required")
	}
	if cfg.OpnsenseWanLL == "" {
		slog.Warn("wan.routes: missing opnsense_wan_ll")
		return fmt.Errorf("wan.routes: opnsense_wan_ll is required")
	}
	if cfg.OpnsenseEdgeV6 == "" {
		slog.Warn("wan.routes: missing opnsense_edge_v6")
		return fmt.Errorf("wan.routes: opnsense_edge_v6 is required")
	}
	if cfg.InternalPrefix == "" {
		slog.Warn("wan.routes: missing internal_prefix")
		return fmt.Errorf("wan.routes: internal_prefix is required")
	}
	if cfg.InternalNetV4 == "" {
		slog.Warn("wan.routes: missing internal_net_v4")
		return fmt.Errorf("wan.routes: internal_net_v4 is required")
	}
	seenNames := make(map[string]bool, len(cfg.WANs))
	seenSlots := map[ruleSlot]bool{}
	for i, wan := range cfg.WANs {
		if err := validateWAN(wan); err != nil {
			return fmt.Errorf("wan.routes.wan[%d]: %w", i, err)
		}
		if seenNames[wan.Name] {
			slog.Warn("wan.routes: duplicate WAN name", "name", wan.Name)
			return fmt.Errorf("wan.routes.wan[%d]: duplicate name %q", i, wan.Name)
		}
		seenNames[wan.Name] = true
		for _, slot := range wanRuleSlots(wan) {
			if seenSlots[slot] {
				slog.Warn("wan.routes: duplicate rule slot",
					"family", slot.family, "priority", slot.priority)
				return fmt.Errorf(
					"wan.routes.wan[%d]: duplicate rule slot family=%s priority=%d",
					i,
					slot.family,
					slot.priority,
				)
			}
			seenSlots[slot] = true
		}
	}
	return nil
}

func validateWAN(wan WAN) error {
	if wan.Name == "" {
		return fmt.Errorf("name is required")
	}
	if wan.Iface == "" {
		return fmt.Errorf("iface is required")
	}
	if wan.TableID <= 0 {
		return fmt.Errorf("table_id must be > 0")
	}
	if wan.FwMark == 0 {
		return fmt.Errorf("fw_mark must be > 0")
	}
	if !isFwMarkPriority(wan.FwMarkPrio) {
		return fmt.Errorf("fw_mark_prio must be one of 100, 200, or 300")
	}
	if !isFromPriority(wan.FromPrio) {
		return fmt.Errorf("from_prio must be one of 55, 56, or 57")
	}
	return nil
}

func wanRuleSlots(wan WAN) []ruleSlot {
	return []ruleSlot{
		{family: familyV4, priority: wan.FwMarkPrio},
		{family: familyV6, priority: wan.FwMarkPrio},
		{family: familyV4, priority: wan.FromPrio},
		{family: familyV6, priority: wan.FromPrio},
	}
}

func isDefaultRouteEvent(event netif.Event) bool {
	if event.Dest != "default" {
		return false
	}
	return event.Kind == netif.EvRouteAdded || event.Kind == netif.EvRouteDeleted
}

func wanEnabled(gateway string, healthState string) bool {
	if gateway == "" {
		return false
	}
	return netif.HealthIsHealthy(healthState)
}

func fallbackEnabled(health netif.HealthStates) bool {
	return !netif.HealthIsHealthy(health.State(wanNameATT)) &&
		!netif.HealthIsHealthy(health.State(wanNameWebpass)) &&
		netif.HealthIsHealthy(health.State(wanNameMonkeybrains))
}

func findWAN(cfg Config, name string) *WAN {
	for i := range cfg.WANs {
		if cfg.WANs[i].Name == name {
			return &cfg.WANs[i]
		}
	}
	return nil
}

func withPrefix(value string, prefix string) string {
	for _, char := range value {
		if char == '/' {
			return value
		}
	}
	return value + "/" + prefix
}

func isFwMarkPriority(priority int) bool {
	return priority == 100 || priority == 200 || priority == 300
}

func isFromPriority(priority int) bool {
	return priority == 55 || priority == 56 || priority == 57
}

// New is the Constructor registered with ifmgr.
func New(cfg ifmgr.ModuleConfig) (ifmgr.Module, error) {
	c := Config{
		InternalIface:   "",
		OpnsenseWanLL:   "",
		OpnsenseEdgeV6:  "",
		InternalPrefix:  "",
		InternalNetV4:   "",
		HealthStateFile: "",
		ShadowMode:      false,
		WANs:            nil,
	}
	if cfg != nil {
		typedConfig, ok := cfg.(Config)
		if !ok {
			return nil, fmt.Errorf("wan.routes: invalid config type %T", cfg)
		}
		c = typedConfig
	}
	if c.HealthStateFile == "" && len(c.WANs) > 0 {
		c.HealthStateFile = netif.DefaultHealthStatePath
	}
	return &Module{
		BaseModule: ifmgr.NewBaseModule(moduleName),
		cfg:        c,
	}, nil
}

func init() { ifmgr.Register(moduleName, New) }
