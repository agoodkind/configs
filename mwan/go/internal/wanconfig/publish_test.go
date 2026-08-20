package wanconfig

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"testing"
)

// recordingPublisher captures the one ReplaceConfig call Publish makes.
type recordingPublisher struct {
	ownedPaths []string
	items      []Item
	calls      int
	err        error
}

func (r *recordingPublisher) ReplaceConfig(_ context.Context, ownedPaths []string, items []Item) error {
	r.calls++
	r.ownedPaths = ownedPaths
	r.items = items
	return r.err
}

// TestPublish_ReplacesOwnedSubtreesWithTheProjection pins the write contract:
// one replace of exactly the owned subtrees carrying the projected items.
func TestPublish_ReplacesOwnedSubtreesWithTheProjection(t *testing.T) {
	t.Parallel()
	gateway := Gateway{
		InternalIface: "eninternal0",
		Members: []Member{{
			Name: "att", Iface: "enatt0", Tier: 0, ProbePolicy: "att",
			NPTInternal: netip.MustParsePrefix("3d06:bad:b01:210::/60"),
			NPTExternal: netip.MustParsePrefix("2001:db8:a::/60"),
		}},
	}
	rec := &recordingPublisher{}

	if err := Publish(context.Background(), slog.Default(), rec, gateway); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if rec.calls != 1 {
		t.Fatalf("ReplaceConfig calls = %d, want 1", rec.calls)
	}
	wantOwned := []string{"/ietf-interfaces:interfaces", "/ietf-nat:nat"}
	if len(rec.ownedPaths) != len(wantOwned) || rec.ownedPaths[0] != wantOwned[0] || rec.ownedPaths[1] != wantOwned[1] {
		t.Fatalf("ownedPaths = %v, want %v", rec.ownedPaths, wantOwned)
	}
	wantItems, err := ConfigItems(gateway)
	if err != nil {
		t.Fatalf("ConfigItems: %v", err)
	}
	if len(rec.items) != len(wantItems) {
		t.Fatalf("items = %d, want %d", len(rec.items), len(wantItems))
	}
}

// TestPublish_NeverWritesAnInvalidGateway pins that a gateway the projection
// rejects reaches the datastore zero times, so a bad config cannot half-write
// the tree.
func TestPublish_NeverWritesAnInvalidGateway(t *testing.T) {
	t.Parallel()
	rec := &recordingPublisher{}
	err := Publish(context.Background(), slog.Default(), rec, Gateway{InternalIface: "", Members: nil})
	if !errors.Is(err, ErrInvalidGateway) {
		t.Fatalf("err = %v, want ErrInvalidGateway", err)
	}
	if rec.calls != 0 {
		t.Fatalf("ReplaceConfig calls = %d, want 0", rec.calls)
	}
}

// TestPublish_SurfacesTheDatastoreFailure pins that a datastore rejection
// comes back to the caller, who logs it and keeps the daemon running.
func TestPublish_SurfacesTheDatastoreFailure(t *testing.T) {
	t.Parallel()
	rejection := errors.New("session start failed")
	rec := &recordingPublisher{err: rejection}
	err := Publish(context.Background(), slog.Default(), rec, Gateway{
		InternalIface: "eninternal0",
		Members:       []Member{{Name: "att", Iface: "enatt0", Tier: 0}},
	})
	if !errors.Is(err, rejection) {
		t.Fatalf("err = %v, want the datastore rejection", err)
	}
}
