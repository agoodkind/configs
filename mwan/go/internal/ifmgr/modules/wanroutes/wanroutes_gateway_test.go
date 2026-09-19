package wanroutes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"goodkind.io/mwan/internal/netif"
	"goodkind.io/mwan/internal/wanstate"
)

type tableFamily struct {
	tableID int
	family  string
}

// fakeRoutingKernel is an in-memory policy-routing table behind the four netif
// route and rule write seams. It holds one rule per family and priority, as the
// module's rule slots do, and counts every write so a test can prove a pass
// touched nothing.
type fakeRoutingKernel struct {
	rules    map[ruleSlot]netif.DesiredRule
	defaults map[tableFamily]string
	prefixes []netif.RouteSpec
	writes   int
}

func newFakeRoutingKernel() *fakeRoutingKernel {
	return &fakeRoutingKernel{
		rules:    map[ruleSlot]netif.DesiredRule{},
		defaults: map[tableFamily]string{},
		prefixes: nil,
		writes:   0,
	}
}

func (f *fakeRoutingKernel) reconcileTableDefault(
	_ context.Context, _ *slog.Logger, want netif.RouteSpec,
) error {
	f.writes++
	key := tableFamily{tableID: want.TableID, family: want.Family}
	if want.Via == "" {
		delete(f.defaults, key)
		return nil
	}
	f.defaults[key] = want.Via
	return nil
}

func (f *fakeRoutingKernel) reconcileTableRoute(
	_ context.Context, _ *slog.Logger, want netif.RouteSpec,
) error {
	f.writes++
	f.prefixes = append(f.prefixes, want)
	return nil
}

func (f *fakeRoutingKernel) reconcileRules(
	_ context.Context, _ *slog.Logger, desired []netif.DesiredRule,
) error {
	f.writes++
	for _, rule := range desired {
		f.rules[ruleSlot{family: rule.Family, priority: rule.Priority}] = rule
	}
	return nil
}

func (f *fakeRoutingKernel) removeRuleAtPriority(
	_ context.Context, _ *slog.Logger, family string, priority int,
) error {
	f.writes++
	delete(f.rules, ruleSlot{family: family, priority: priority})
	return nil
}

// gatewaysByIface answers the gateway seam from testGateways, and reads the
// named missing links through the real netif reader, so their error is the one
// netlink returns for a link the test process does not have.
func gatewaysByIface(cfg Config, missing ...string) func(family string, iface string) (string, error) {
	byIface := make(map[string]gatewaySet, len(cfg.WANs))
	for _, wan := range cfg.WANs {
		byIface[wan.Iface] = testGateways()[wan.Name]
	}
	return func(family string, iface string) (string, error) {
		for _, name := range missing {
			if name == iface {
				return netif.IfaceDefaultGateway(family, iface)
			}
		}
		if family == familyV4 {
			return byIface[iface].V4, nil
		}
		return byIface[iface].V6, nil
	}
}

// newSteeringModule wires a module whose every kernel seam is a fake, with a
// health file path that does not exist, which the reader treats as no verdict
// recorded, so every provider reads healthy.
func newSteeringModule(
	t *testing.T,
	kernel *fakeRoutingKernel,
	store *wanstate.Store,
	defaultGateway func(family string, iface string) (string, error),
) *Module {
	t.Helper()
	cfg := testConfig()
	cfg.HealthStateFile = filepath.Join(t.TempDir(), "mwan-health.state")
	module := &Module{cfg: cfg}
	module.InitBase(testEnvWithStore(store), "module", moduleName)
	module.resolveNextHop = func(context.Context, *slog.Logger, string, string) (bool, error) {
		return true, nil
	}
	module.defaultGateway = defaultGateway
	module.reconcileTableDefault = kernel.reconcileTableDefault
	module.reconcileTableRoute = kernel.reconcileTableRoute
	module.reconcileRules = kernel.reconcileRules
	module.removeRuleAtPriority = kernel.removeRuleAtPriority
	return module
}

func rulesForTables(rules []netif.DesiredRule, tableIDs ...int) map[ruleSlot]netif.DesiredRule {
	kept := map[ruleSlot]netif.DesiredRule{}
	for _, rule := range rules {
		for _, tableID := range tableIDs {
			if rule.TableID == tableID {
				kept[ruleSlot{family: rule.Family, priority: rule.Priority}] = rule
			}
		}
	}
	return kept
}

// TestReconcileSteersPastAMissingLink detaches Monkeybrains' link while its
// rules from an earlier pass are still installed. The pass must remove those
// rules, install AT&T's and Webpass's rules and default routes, report neither
// of them lost, and return an error naming Monkeybrains in both families.
func TestReconcileSteersPastAMissingLink(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := testConfig()
	kernel := newFakeRoutingKernel()
	for slot, rule := range rulesForTables(allHealthyRules(cfg), 300) {
		kernel.rules[slot] = rule
	}
	store := wanstate.New()
	module := newSteeringModule(t, kernel, store, gatewaysByIface(cfg, "mbrains0"))

	err := module.Reconcile(ctx, module.Log)
	if err == nil {
		t.Fatal("Reconcile returned nil, want an error naming the missing monkeybrains link")
	}
	for _, want := range []string{"monkeybrains inet default gateway", "monkeybrains inet6 default gateway"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Reconcile error = %v, want it to contain %q", err, want)
		}
	}
	for _, unwanted := range []string{"att inet", "att inet6", "webpass inet", "webpass inet6"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Fatalf("Reconcile error = %v, want no error for a present link (%q)", err, unwanted)
		}
	}

	wantRules := rulesForTables(allHealthyRules(cfg), 100, 200)
	if !reflect.DeepEqual(kernel.rules, wantRules) {
		t.Fatalf("installed rules\ngot:  %#v\nwant: %#v", kernel.rules, wantRules)
	}
	wantDefaults := map[tableFamily]string{
		{tableID: 100, family: familyV4}: "192.0.2.1",
		{tableID: 100, family: familyV6}: "fe80::a",
		{tableID: 200, family: familyV4}: "203.0.113.1",
		{tableID: 200, family: familyV6}: "fe80::b",
	}
	if !reflect.DeepEqual(kernel.defaults, wantDefaults) {
		t.Fatalf("table defaults\ngot:  %#v\nwant: %#v", kernel.defaults, wantDefaults)
	}

	routing := store.Snapshot().Routing
	wantCarrying := map[string]bool{"att": true, "webpass": true, "monkeybrains": false}
	for name, carrying := range wantCarrying {
		if routing[name].Carrying != carrying {
			t.Fatalf("%s carrying = %v, want %v", name, routing[name].Carrying, carrying)
		}
	}
}

// TestReconcileStopsOnAGatewayReadFailure fails one gateway read with an error
// that is not a missing link. The pass must stop before any route or rule
// write, so the rules already installed stay exactly as they were.
func TestReconcileStopsOnAGatewayReadFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := testConfig()
	kernel := newFakeRoutingKernel()
	installed := rulesForTables(allHealthyRules(cfg), 100, 200, 300)
	for slot, rule := range installed {
		kernel.rules[slot] = rule
	}
	readFailure := errors.New("netlink receive: resource temporarily unavailable")
	presentGateways := gatewaysByIface(cfg)
	failingGateway := func(family string, iface string) (string, error) {
		if iface == "webpass0" && family == familyV6 {
			return "", fmt.Errorf("RouteListFiltered(iface-default,%s,%s): %w", iface, family, readFailure)
		}
		return presentGateways(family, iface)
	}
	module := newSteeringModule(t, kernel, wanstate.New(), failingGateway)

	err := module.Reconcile(ctx, module.Log)
	if !errors.Is(err, readFailure) || !strings.Contains(err.Error(), "webpass inet6 default gateway") {
		t.Fatalf("Reconcile error = %v, want the webpass inet6 read failure", err)
	}
	if kernel.writes != 0 {
		t.Fatalf("kernel writes = %d, want none after a failed gateway read", kernel.writes)
	}
	if !reflect.DeepEqual(kernel.rules, installed) {
		t.Fatalf("installed rules changed\ngot:  %#v\nwant: %#v", kernel.rules, installed)
	}
}
