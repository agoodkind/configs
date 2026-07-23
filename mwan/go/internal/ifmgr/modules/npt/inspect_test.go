//go:build linux

package npt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"reflect"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

type fakeReadConn struct {
	rules map[string][]*nftables.Rule
	errs  map[string]error
}

func (f *fakeReadConn) GetRules(
	_ *nftables.Table,
	chain *nftables.Chain,
) ([]*nftables.Rule, error) {
	return f.rules[chain.Name], f.errs[chain.Name]
}

func TestRenderTableRoundTripsRuleExprs(t *testing.T) {
	t.Parallel()

	iface := "enatt0.3242"
	publicPrefix := netip.MustParsePrefix("2600:1700:2f71:c80::/60")
	internalPrefix := netip.MustParsePrefix("3d06:bad:b01::/60")
	publicAddress := netip.MustParseAddr("2600:1700:2f71:c80::1")
	internalAddress := netip.MustParseAddr("3d06:bad:b01:fe::2")

	prerouting := []*nftables.Rule{
		encodedRuleForTest(t, natRule{
			Chain:  chainPrerouting,
			Iface:  iface,
			Match:  netip.PrefixFrom(publicAddress, 128),
			Op:     opDNAT,
			ToAddr: internalAddress,
		}),
		encodedRuleForTest(t, natRule{
			Chain: chainPrerouting,
			Iface: iface,
			Match: publicPrefix,
			Op:    opDNATPrefix,
			ToPfx: internalPrefix,
		}),
	}
	postrouting := []*nftables.Rule{
		encodedRuleForTest(t, natRule{
			Chain: chainPostrouting,
			Iface: iface,
			Match: netip.PrefixFrom(internalAddress, 128),
			Op:    opGuard,
		}),
		encodedRuleForTest(t, natRule{
			Chain:  chainPostrouting,
			Iface:  iface,
			Match:  netip.PrefixFrom(internalAddress, 128),
			Op:     opSNAT,
			ToAddr: publicAddress,
		}),
		encodedRuleForTest(t, natRule{
			Chain: chainPostrouting,
			Iface: iface,
			Match: internalPrefix,
			Op:    opSNATPrefix,
			ToPfx: publicPrefix,
		}),
	}

	fake := &fakeReadConn{
		rules: map[string][]*nftables.Rule{
			preroutingChain:  prerouting,
			postroutingChain: postrouting,
		},
		errs: nil,
	}
	reader := &nftReader{newConn: func() (nftReadConn, error) {
		return fake, nil
	}}
	got, err := reader.renderTable(context.Background(), discardLogger())
	if err != nil {
		t.Fatalf("renderTable returned error: %v", err)
	}

	want := RenderedTable{
		Prerouting: []string{
			`iif "enatt0.3242" ip6 daddr 2600:1700:2f71:c80::1 dnat to 3d06:bad:b01:fe::2`,
			`iif "enatt0.3242" ip6 daddr 2600:1700:2f71:c80::/60 dnat prefix to 3d06:bad:b01::/60`,
		},
		Postrouting: []string{
			`oif "enatt0.3242" ip6 saddr 3d06:bad:b01:fe::2 ct status dnat return`,
			`oif "enatt0.3242" ip6 saddr 3d06:bad:b01:fe::2 snat to 2600:1700:2f71:c80::1`,
			`oif "enatt0.3242" ip6 saddr 3d06:bad:b01::/60 snat prefix to 2600:1700:2f71:c80::/60`,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderTable mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestRenderTableFallsBackForUnrecognizedRule(t *testing.T) {
	t.Parallel()

	fake := &fakeReadConn{
		rules: map[string][]*nftables.Rule{
			preroutingChain: {
				{Exprs: []expr.Any{&expr.Counter{}}},
			},
		},
		errs: nil,
	}
	reader := &nftReader{newConn: func() (nftReadConn, error) {
		return fake, nil
	}}
	got, err := reader.renderTable(context.Background(), discardLogger())
	if err != nil {
		t.Fatalf("renderTable returned error: %v", err)
	}
	want := []string{"# unrecognized rule (1 exprs)"}
	if !reflect.DeepEqual(got.Prerouting, want) {
		t.Fatalf("prerouting = %v, want %v", got.Prerouting, want)
	}
}

func TestRenderTableTreatsMissingTableAsEmpty(t *testing.T) {
	t.Parallel()

	fake := &fakeReadConn{
		rules: nil,
		errs: map[string]error{
			preroutingChain: fmt.Errorf("receiveAckAware: %w", unix.ENOENT),
		},
	}
	reader := &nftReader{newConn: func() (nftReadConn, error) {
		return fake, nil
	}}
	got, err := reader.renderTable(context.Background(), discardLogger())
	if err != nil {
		t.Fatalf("renderTable returned error: %v", err)
	}
	if !reflect.DeepEqual(got, emptyRenderedTable()) {
		t.Fatalf("renderTable = %#v, want empty table", got)
	}
}

func TestRenderTableRejectsRandomizedNAT(t *testing.T) {
	t.Parallel()

	rule := encodedRuleForTest(t, natRule{
		Chain:  chainPostrouting,
		Iface:  "enatt0",
		Match:  netip.MustParsePrefix("3d06:bad:b01:fe::2/128"),
		Op:     opSNAT,
		ToAddr: netip.MustParseAddr("2600:1700:2f71:c80::1"),
	})
	for _, expression := range rule.Exprs {
		if nat, ok := expression.(*expr.NAT); ok {
			nat.Random = true
		}
	}

	got := renderChainForTest(t, postroutingChain, rule)
	want := []string{fmt.Sprintf("# unrecognized rule (%d exprs)", len(rule.Exprs))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("render = %v, want %v", got, want)
	}
}

func TestRenderTableRejectsEqualPrefixRegisters(t *testing.T) {
	t.Parallel()

	rule := encodedRuleForTest(t, natRule{
		Chain: chainPostrouting,
		Iface: "enatt0",
		Match: netip.MustParsePrefix("3d06:bad:b01::/60"),
		Op:    opSNATPrefix,
		ToPfx: netip.MustParsePrefix("2600:1700:2f71:c80::/60"),
	})
	// Collapse both NETMAP range registers onto register 1, the runtime shape
	// where the second Immediate overwrites the first.
	for _, expression := range rule.Exprs {
		switch typed := expression.(type) {
		case *expr.Immediate:
			typed.Register = 1
		case *expr.NAT:
			typed.RegAddrMin = 1
			typed.RegAddrMax = 1
		}
	}

	got := renderChainForTest(t, postroutingChain, rule)
	want := []string{fmt.Sprintf("# unrecognized rule (%d exprs)", len(rule.Exprs))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("render = %v, want %v", got, want)
	}
}

func renderChainForTest(t *testing.T, chainName string, rules ...*nftables.Rule) []string {
	t.Helper()

	fake := &fakeReadConn{
		rules: map[string][]*nftables.Rule{chainName: rules},
		errs:  nil,
	}
	reader := &nftReader{newConn: func() (nftReadConn, error) {
		return fake, nil
	}}
	got, err := reader.renderTable(context.Background(), discardLogger())
	if err != nil {
		t.Fatalf("renderTable returned error: %v", err)
	}
	if chainName == preroutingChain {
		return got.Prerouting
	}
	return got.Postrouting
}

func encodedRuleForTest(t *testing.T, rule natRule) *nftables.Rule {
	t.Helper()

	expressions, err := ruleExprs(rule)
	if err != nil {
		t.Fatalf("ruleExprs(%s) returned error: %v", rule, err)
	}
	return &nftables.Rule{Exprs: expressions}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

var _ nftReadConn = (*fakeReadConn)(nil)
