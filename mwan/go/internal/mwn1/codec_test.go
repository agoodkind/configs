package mwn1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// mustOpnsenseRegistry constructs the canonical mwan_opnsense
// registry and fails the test if registration errors. Used by codec
// tests so each case stays a single line.
func mustOpnsenseRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := NewMWANOPNsenseRegistry()
	if err != nil {
		t.Fatalf("NewMWANOPNsenseRegistry: %v", err)
	}
	return reg
}

func TestCodec_RequestRoundTrip(t *testing.T) {
	reg := mustOpnsenseRegistry(t)
	want := &mwanv1.ExecRequest{
		Command:        "/bin/echo",
		Args:           []string{"hi", "there"},
		Sudo:           true,
		TimeoutSeconds: 30,
		StdinBytes:     []byte("input"),
	}
	out, id, err := MarshalRequest(reg, MethodExec, want)
	if err != nil {
		t.Fatalf("MarshalRequest: %v", err)
	}
	if id != MethodExec {
		t.Fatalf("returned id=%d want %d", id, MethodExec)
	}
	got, err := UnmarshalRequest(reg, MethodExec, out)
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Fatalf("round-trip diff (-want +got):\n%s", diff)
	}
}

func TestCodec_ResponseRoundTrip(t *testing.T) {
	reg := mustOpnsenseRegistry(t)
	want := &mwanv1.VersionResponse{
		Version:      "v1.2.3",
		BuildCommit:  "deadbeef",
		BuildDirty:   false,
		BuildBinhash: "abc",
	}
	out, _, err := MarshalResponse(reg, MethodVersion, want)
	if err != nil {
		t.Fatalf("MarshalResponse: %v", err)
	}
	got, err := UnmarshalResponse(reg, MethodVersion, out)
	if err != nil {
		t.Fatalf("UnmarshalResponse: %v", err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Fatalf("round-trip diff (-want +got):\n%s", diff)
	}
}

func TestCodec_UnknownMethodID(t *testing.T) {
	reg := mustOpnsenseRegistry(t)
	if _, err := UnmarshalRequest(reg, 9999, []byte{}); err == nil {
		t.Fatalf("expected error for unknown id")
	}
	if _, _, err := MarshalRequest(reg, 9999, &mwanv1.VersionRequest{}); err == nil {
		t.Fatalf("expected error for unknown id")
	}
	if _, err := UnmarshalResponse(reg, 9999, []byte{}); err == nil {
		t.Fatalf("expected error for unknown id")
	}
	if _, _, err := MarshalResponse(reg, 9999, &mwanv1.VersionResponse{}); err == nil {
		t.Fatalf("expected error for unknown id")
	}
}

func TestCodec_EmptyPayloadUnmarshalsZeroValue(t *testing.T) {
	reg := mustOpnsenseRegistry(t)
	msg, err := UnmarshalRequest(reg, MethodVersion, nil)
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}
	if _, ok := msg.(*mwanv1.VersionRequest); !ok {
		t.Fatalf("got %T, want *VersionRequest", msg)
	}
}

func TestCodec_NilMessageRejected(t *testing.T) {
	reg := mustOpnsenseRegistry(t)
	if _, _, err := MarshalRequest(reg, MethodVersion, nil); err == nil {
		t.Fatalf("nil request must error")
	}
	if _, _, err := MarshalResponse(reg, MethodVersion, nil); err == nil {
		t.Fatalf("nil response must error")
	}
}
