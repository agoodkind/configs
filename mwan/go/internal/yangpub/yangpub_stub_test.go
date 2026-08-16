//go:build linux && !cgo

package yangpub

import (
	"errors"
	"log/slog"
	"testing"
)

// TestNewReportsUnavailableWithoutBinding pins the contract a cgo-off linux
// build serves: the constructor yields no publisher and an error callers
// can recognize, so a daemon built without the binding keeps running
// instead of failing to start.
func TestNewReportsUnavailableWithoutBinding(t *testing.T) {
	publisher, err := New(slog.New(slog.DiscardHandler))
	if publisher != nil {
		t.Fatalf("New returned a publisher = %#v, want nil", publisher)
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("New error = %v, want one matching ErrUnavailable", err)
	}
}
