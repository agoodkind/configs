//go:build linux

// Package yangpub publishes daemon state into the wanconfig management
// datastore (sysrepo) and registers operational providers. It carries
// exactly what publishing needs: connect, open a session, set values by
// path, apply, and provider registration. The surface accepts no edits,
// so the package holds no write acceptance, no validation, and no
// transaction machinery beyond apply.
//
// The package is linux-only. sysrepo requires robust pthread mutexes,
// which darwin lacks, and the FreeBSD router build has no management
// surface to publish into, so those binaries leave the package out entirely
// rather than carrying a stub of a feature they can never use; the one
// caller in cmd/mwan is split by platform the same way. On linux the real
// implementation binds libsysrepo through cgo, and the go-makefile cgo hook
// provisions the pinned libraries where the gates run, so linux CI checks
// these files fully. A linux build with cgo off gets a stub whose
// constructor returns ErrUnavailable, so a development build still compiles
// and reports why it cannot publish; the release guard rejects shipping it.
package yangpub

import (
	"context"
	"errors"
)

// ErrUnavailable means this binary was built without the sysrepo
// binding. The caller keeps running without a management surface; the
// data path is never gated on it.
var ErrUnavailable = errors.New("yangpub: built without the sysrepo binding")

// Datastore names a sysrepo datastore a publish targets.
type Datastore string

const (
	// DatastoreRunning is the running configuration datastore.
	DatastoreRunning Datastore = "running"
	// DatastoreOperational is the read-only operational datastore.
	DatastoreOperational Datastore = "operational"
)

// Item is one path-value pair to publish.
type Item struct {
	Path  string
	Value string
}

// ProviderFunc computes the items under xpath at the moment a read
// arrives, so the served tree cannot drift from the daemon.
type ProviderFunc func(ctx context.Context, xpath string) ([]Item, error)

// Publisher is the daemon's handle on the management datastore.
type Publisher interface {
	// GetItem reads one value by path in ds. It reports found=false when
	// the path holds no value, which lets a publisher leave state it did
	// not own unchanged.
	GetItem(ctx context.Context, ds Datastore, path string) (value string, found bool, err error)
	// SetItems sets every item by path in ds and applies them as one
	// change.
	SetItems(ctx context.Context, ds Datastore, items []Item) error
	// DeleteItem removes the value at path in ds and applies the change.
	DeleteItem(ctx context.Context, ds Datastore, path string) error
	// RegisterProvider registers fn as the operational provider of
	// xpath inside module, so a read reaches the daemon at request
	// time. Registrations live until Close.
	RegisterProvider(ctx context.Context, module string, xpath string, fn ProviderFunc) error
	// Close releases every registration and the connection.
	Close() error
}
