package opnsensesvc

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/mwn1"
)

// TestChannelSplit_ShortDispatcherRejectsLongMethods proves the
// dispatcher returns FlagError for any methodID outside its
// AllowedMethods set. The short-channel dispatcher accepts Version
// but refuses Exec; the error message names the rejected id so the
// operator sees which RPC was sent to the wrong port.
func TestChannelSplit_ShortDispatcherRejectsLongMethods(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcherWithAllowedMethods(t, srv, ShortChannelMethods)
	defer stop()

	// Version is on the short channel: should succeed.
	resp := client.call(t, mwn1.MethodVersion, 700, &mwanv1.VersionRequest{})
	assertNoErrorFrame(t, resp)

	// Exec is on the long channel: short dispatcher must reject.
	exec := client.call(t, mwn1.MethodExec, 701, &mwanv1.ExecRequest{
		Command: "/bin/echo",
		Args:    []string{"ok"},
	})
	assertErrorMessageContains(t, exec, "not allowed on this port")
	if exec.flags&mwn1.FlagError == 0 {
		t.Fatalf("expected FlagError on Exec rejection, got flags=%x", exec.flags)
	}
}

// TestChannelSplit_LongDispatcherRejectsShortMethods is the
// symmetric assertion: a long-channel dispatcher refuses
// short-channel RPCs. The error path uses the same gating code so
// this test mostly guards against the AllowedMethods set being
// mis-built at construction time.
func TestChannelSplit_LongDispatcherRejectsShortMethods(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcherWithAllowedMethods(t, srv, LongChannelMethods)
	defer stop()

	exec := client.call(t, mwn1.MethodExec, 710, &mwanv1.ExecRequest{
		Command: "/bin/echo",
		Args:    []string{"ok"},
	})
	assertNoErrorFrame(t, exec)

	ver := client.call(t, mwn1.MethodVersion, 711, &mwanv1.VersionRequest{})
	assertErrorMessageContains(t, ver, "not allowed on this port")
}

// TestChannelSplit_NilAllowedAcceptsEverything preserves the
// pre-Fix-4 single-port behavior: a Dispatcher built with
// AllowedMethods=nil accepts every method id without gating. The
// test cares only about the gating verdict; handler-level errors
// from the test server are tolerated.
func TestChannelSplit_NilAllowedAcceptsEverything(t *testing.T) {
	srv := newTestServer(t, dispatcherSampleConfig())
	client, stop := startDispatcherWithAllowedMethods(t, srv, nil)
	defer stop()

	reg := newRegistryOrFail(t)
	for _, m := range append(append([]uint16{}, ShortChannelMethods...), LongChannelMethods...) {
		req, ok := reg.NewRequest(m)
		if !ok || req == nil {
			t.Fatalf("registry missing request factory for method id %d", m)
		}
		seedRequestForMethod(m, req)
		resp := client.call(t, m, 730+uint64(m), req)
		if resp.flags&mwn1.FlagError != 0 && strings.Contains(string(resp.payload), "not allowed on this port") {
			t.Fatalf("method %d rejected by gating when AllowedMethods=nil", m)
		}
	}
}

// seedRequestForMethod fills in the minimum required fields on a
// freshly-constructed request so the handler does not reject before
// reaching the gating check. The point of the surrounding test is
// the gating verdict, not handler validation.
func seedRequestForMethod(methodID uint16, req proto.Message) {
	switch methodID {
	case mwn1.MethodXPathGet:
		if r, ok := req.(*mwanv1.XPathGetRequest); ok {
			r.Expression = "/opnsense"
		}
	case mwn1.MethodXPathSet:
		if r, ok := req.(*mwanv1.XPathSetRequest); ok {
			r.Expression = "/opnsense/system/hostname"
			r.NewValue = "h"
		}
	case mwn1.MethodXPathDelete:
		if r, ok := req.(*mwanv1.XPathDeleteRequest); ok {
			r.Expression = "/opnsense/system/nonexistent"
		}
	case mwn1.MethodWriteConfigXML:
		if r, ok := req.(*mwanv1.WriteConfigXMLRequest); ok {
			r.Content = dispatcherSampleConfig()
		}
	case mwn1.MethodExec:
		if r, ok := req.(*mwanv1.ExecRequest); ok {
			r.Command = "/bin/echo"
			r.Args = []string{"ok"}
		}
	case mwn1.MethodInjectGatewayV6:
		if r, ok := req.(*mwanv1.InjectGatewayV6Request); ok {
			r.GatewayName = "WAN_GW6"
		}
	}
}
