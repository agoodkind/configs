package mwn1

import (
	"testing"

	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(42, "test/Foo",
		func() proto.Message { return &mwanv1.VersionRequest{} },
		func() proto.Message { return &mwanv1.VersionResponse{} }); err != nil {
		t.Fatalf("register: %v", err)
	}
	id, ok := reg.MethodID("test/Foo")
	if !ok || id != 42 {
		t.Fatalf("MethodID: got id=%d ok=%v", id, ok)
	}
	name, ok := reg.MethodName(42)
	if !ok || name != "test/Foo" {
		t.Fatalf("MethodName: got name=%q ok=%v", name, ok)
	}
	if msg, ok := reg.NewRequest(42); !ok || msg == nil {
		t.Fatalf("NewRequest: ok=%v msg=%v", ok, msg)
	}
	if msg, ok := reg.NewResponse(42); !ok || msg == nil {
		t.Fatalf("NewResponse: ok=%v msg=%v", ok, msg)
	}
}

func TestRegistry_DuplicateID(t *testing.T) {
	reg := NewRegistry()
	mk := func() proto.Message { return &mwanv1.VersionRequest{} }
	if err := reg.Register(1, "a", mk, mk); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := reg.Register(1, "b", mk, mk); err == nil {
		t.Fatalf("duplicate id must error")
	}
}

func TestRegistry_DuplicateName(t *testing.T) {
	reg := NewRegistry()
	mk := func() proto.Message { return &mwanv1.VersionRequest{} }
	if err := reg.Register(1, "a", mk, mk); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := reg.Register(2, "a", mk, mk); err == nil {
		t.Fatalf("duplicate name must error")
	}
}

func TestRegistry_ZeroIDReserved(t *testing.T) {
	reg := NewRegistry()
	mk := func() proto.Message { return &mwanv1.VersionRequest{} }
	if err := reg.Register(0, "x", mk, mk); err == nil {
		t.Fatalf("id=0 must error")
	}
}

func TestRegistry_NilFactoryRejected(t *testing.T) {
	reg := NewRegistry()
	mk := func() proto.Message { return &mwanv1.VersionRequest{} }
	if err := reg.Register(1, "x", nil, mk); err == nil {
		t.Fatalf("nil request factory must error")
	}
	if err := reg.Register(2, "y", mk, nil); err == nil {
		t.Fatalf("nil response factory must error")
	}
}

func TestRegistry_UnknownLookups(t *testing.T) {
	reg := NewRegistry()
	if _, ok := reg.MethodID("nope"); ok {
		t.Fatalf("expected miss")
	}
	if _, ok := reg.MethodName(99); ok {
		t.Fatalf("expected miss")
	}
	if _, ok := reg.NewRequest(99); ok {
		t.Fatalf("expected miss")
	}
	if _, ok := reg.NewResponse(99); ok {
		t.Fatalf("expected miss")
	}
}

// expectedMWANOPNsenseMethods is the canonical declaration order of
// RPCs in mwan_opnsense.proto. The test asserts ids 1..N match.
var expectedMWANOPNsenseMethods = []string{
	"Version",
	"Exec",
	"ReadConfigXML",
	"WriteConfigXML",
	"BackupConfigXML",
	"XPathGet",
	"XPathSet",
	"XPathDelete",
	"StripGatewayV6",
	"InjectGatewayV6",
	"Deploy",
	"DeployStatus",
	"Revert",
	"Reset",
}

func TestRegistry_MWANOPNsenseAssignments(t *testing.T) {
	reg, err := NewMWANOPNsenseRegistry()
	if err != nil {
		t.Fatalf("NewMWANOPNsenseRegistry: %v", err)
	}
	for i, m := range expectedMWANOPNsenseMethods {
		wantID := uint16(i + 1)
		gotID, ok := reg.MethodID(MWANOPNsenseServicePrefix + m)
		if !ok {
			t.Fatalf("missing method %s", m)
		}
		if gotID != wantID {
			t.Fatalf("%s: got id=%d want %d", m, gotID, wantID)
		}
		if req, ok := reg.NewRequest(wantID); !ok || req == nil {
			t.Fatalf("%s: missing request factory", m)
		}
		if resp, ok := reg.NewResponse(wantID); !ok || resp == nil {
			t.Fatalf("%s: missing response factory", m)
		}
	}
}
