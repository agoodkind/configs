//go:build linux

package wanroutes

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"

	"goodkind.io/mwan/internal/netif"
	"goodkind.io/mwan/internal/wanstate"
)

// fakeLinkAddresses is an in-memory kernel address table behind the two netif
// address seams. Its write replaces whatever a link holds at the same address,
// which is what the kernel's address replace does, so a module that asked for
// the link address at /32 would visibly rewrite the link's own prefix here.
type fakeLinkAddresses struct {
	byIface map[string][]string
	listErr map[string]error
}

func (f *fakeLinkAddresses) list(
	_ context.Context, _ *slog.Logger, iface string,
) ([]netif.CurrentAddr, error) {
	if err := f.listErr[iface]; err != nil {
		return nil, err
	}
	held := make([]netif.CurrentAddr, 0, len(f.byIface[iface]))
	for _, cidr := range f.byIface[iface] {
		family := familyV4
		if strings.Contains(cidr, ":") {
			family = familyV6
		}
		held = append(held, netif.CurrentAddr{CIDR: cidr, Family: family, Flags: 0})
	}
	return held, nil
}

func (f *fakeLinkAddresses) reconcile(
	_ context.Context, _ *slog.Logger, iface string, desired []netif.AddrSpec,
) error {
	for _, spec := range desired {
		wanted := netip.MustParsePrefix(spec.CIDR)
		kept := make([]string, 0, len(f.byIface[iface])+1)
		for _, cidr := range f.byIface[iface] {
			if netip.MustParsePrefix(cidr).Addr() != wanted.Addr() {
				kept = append(kept, cidr)
			}
		}
		f.byIface[iface] = append(kept, spec.CIDR)
	}
	return nil
}

func addresses(values ...string) []netip.Addr {
	parsed := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		parsed = append(parsed, netip.MustParseAddr(value))
	}
	return parsed
}

// newOwnershipModule gives att a routed block outside its lease's subnet and
// webpass an on-link block whose first mapped address is the link address, the
// two shapes the production providers have.
func newOwnershipModule(kernel *fakeLinkAddresses, store *wanstate.Store) *Module {
	cfg := testConfig()
	cfg.WANs = append([]WAN(nil), cfg.WANs...)
	cfg.WANs[0].MappedExternals = addresses("198.51.100.193", "198.51.100.194")
	cfg.WANs[1].MappedExternals = addresses("203.0.113.2", "203.0.113.3", "203.0.113.4")
	module := &Module{cfg: cfg}
	module.InitBase(testEnvWithStore(store), "module", moduleName)
	module.listAddrs = kernel.list
	module.reconcileAddrs = kernel.reconcile
	return module
}

func (m *Module) runOwnership(ctx context.Context, log *slog.Logger) error {
	m.Lock()
	defer m.Unlock()
	return m.ownMappedAddressesLocked(ctx, log)
}

func sortedCopy(values []string) []string {
	copied := slices.Clone(values)
	slices.Sort(copied)
	return copied
}

func TestOwnMappedAddressesOwnsOnlyOnLinkAddresses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kernel := &fakeLinkAddresses{
		byIface: map[string][]string{
			"att0":     {"192.0.2.10/24"},
			"webpass0": {"203.0.113.2/29", "fe80::b/64"},
			"mbrains0": {"198.51.100.10/24"},
		},
		listErr: map[string]error{},
	}
	store := wanstate.New()
	module := newOwnershipModule(kernel, store)

	// A second pass must find the /32s it added and leave both the link and the
	// served set unchanged.
	for pass := 1; pass <= 2; pass++ {
		if err := module.runOwnership(ctx, module.Log); err != nil {
			t.Fatalf("pass %d: ownership returned error: %v", pass, err)
		}
	}
	module.publishLiveState(testGateways(), netif.HealthStates{})

	wantWebpass := []string{"203.0.113.2/29", "203.0.113.3/32", "203.0.113.4/32", "fe80::b/64"}
	if got := sortedCopy(kernel.byIface["webpass0"]); !reflect.DeepEqual(got, wantWebpass) {
		t.Fatalf("webpass0 addresses = %v, want %v", got, wantWebpass)
	}
	if got := kernel.byIface["att0"]; !reflect.DeepEqual(got, []string{"192.0.2.10/24"}) {
		t.Fatalf("att0 addresses = %v, want the lease alone", got)
	}
	routing := store.Snapshot().Routing
	if got := routing["webpass"].OwnedAddresses; !reflect.DeepEqual(got, addresses("203.0.113.3", "203.0.113.4")) {
		t.Fatalf("webpass owned addresses = %v, want 203.0.113.3 and 203.0.113.4", got)
	}
	if got := routing["att"].OwnedAddresses; len(got) != 0 {
		t.Fatalf("att owned addresses = %v, want none for a routed block", got)
	}
	if got := routing["monkeybrains"].OwnedAddresses; len(got) != 0 {
		t.Fatalf("monkeybrains owned addresses = %v, want none", got)
	}
}

func TestOwnMappedAddressesContinuesPastAnUnreadableLink(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kernel := &fakeLinkAddresses{
		byIface: map[string][]string{
			"webpass0": {"203.0.113.2/29"},
		},
		listErr: map[string]error{"att0": errors.New("link not found")},
	}
	store := wanstate.New()
	module := newOwnershipModule(kernel, store)

	err := module.runOwnership(ctx, module.Log)
	if err == nil || !strings.Contains(err.Error(), "att0") {
		t.Fatalf("ownership error = %v, want one naming att0", err)
	}
	module.publishLiveState(testGateways(), netif.HealthStates{})

	routing := store.Snapshot().Routing
	if got := routing["webpass"].OwnedAddresses; !reflect.DeepEqual(got, addresses("203.0.113.3", "203.0.113.4")) {
		t.Fatalf("webpass owned addresses = %v, want 203.0.113.3 and 203.0.113.4", got)
	}
	if got := routing["att"].OwnedAddresses; len(got) != 0 {
		t.Fatalf("att owned addresses = %v, want none when its link cannot be read", got)
	}
}
