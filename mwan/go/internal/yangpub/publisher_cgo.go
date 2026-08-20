//go:build linux && cgo

package yangpub

/*
#cgo pkg-config: sysrepo libyang
#include <stdlib.h>
#include <sysrepo.h>
#include <sysrepo/values.h>
#include <libyang/libyang.h>

extern int yangpubOperCB(sr_session_ctx_t *session, uint32_t sub_id, const char *module_name,
        const char *path, const char *request_xpath, uint32_t operation_id,
        struct lyd_node **parent, void *private_data);
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/cgo"
	"sync"
	"unsafe"
)

// applyTimeoutMS bounds sr_apply_changes so a stuck datastore cannot
// stall the caller indefinitely.
const applyTimeoutMS = 5000

// getTimeoutMS bounds sr_get_item so a stuck datastore cannot stall a
// read indefinitely.
const getTimeoutMS = 5000

// ErrClosed means the publisher was already closed. Every entry point
// rejects a closed publisher rather than passing a freed connection back
// into sysrepo.
var ErrClosed = errors.New("yangpub: publisher is closed")

// publisher is the cgo-backed Publisher over one sysrepo connection.
// Every field below mu is guarded by it, including the connection itself,
// because Close frees the connection and any later call must see that.
type publisher struct {
	log *slog.Logger

	mu            sync.Mutex
	conn          *C.sr_conn_ctx_t
	closed        bool
	subscriptions []*operSubscription
}

// connection returns the live connection, or an error once Close ran.
// The caller holds no lock; this takes and releases mu itself.
func (p *publisher) connection() (*C.sr_conn_ctx_t, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.conn == nil {
		return nil, ErrClosed
	}
	return p.conn, nil
}

// operSubscription holds everything one provider registration owns: the
// long-lived session the subscription is tied to, the subscription
// itself, the cgo handle the C callback resolves, and the C-allocated
// cell that carries the handle across the language boundary.
type operSubscription struct {
	session      *C.sr_session_ctx_t
	subscription *C.sr_subscription_ctx_t
	handle       cgo.Handle
	handleCell   unsafe.Pointer
}

// providerReg is the value the C callback recovers through its handle.
type providerReg struct {
	log *slog.Logger
	fn  ProviderFunc
}

// New connects to sysrepo and returns the cgo-backed publisher. A failed
// connect leaves nothing behind; the caller keeps running without a
// management surface.
func New(log *slog.Logger) (Publisher, error) {
	var conn *C.sr_conn_ctx_t
	if rc := C.sr_connect(C.sr_conn_options_t(0), &conn); srFailed(rc) {
		connectErr := srError("sr_connect", rc)
		log.Error("sysrepo connect failed", "err", connectErr)
		return nil, connectErr
	}
	return &publisher{
		log:           log,
		mu:            sync.Mutex{},
		conn:          conn,
		closed:        false,
		subscriptions: nil,
	}, nil
}

// srErrorString renders a sysrepo return code as its message.
func srErrorString(returnCode C.int) string {
	return C.GoString(C.sr_strerror(returnCode))
}

// srFailed reports whether a sysrepo call returned anything other than
// success. Every call site reads the same way through it, so no caller
// open-codes the comparison against the success constant.
func srFailed(returnCode C.int) bool {
	return returnCode != C.SR_ERR_OK
}

// srError names the failed sysrepo call and carries its message, so a
// caller returns and logs one value instead of formatting the same text
// twice.
func srError(operation string, returnCode C.int) error {
	return fmt.Errorf("yangpub: %s: %s", operation, srErrorString(returnCode))
}

// startSession opens a sysrepo session on ds over the live connection.
// Every entry point needs one, and each previously repeated the same
// call, check, and error wording.
func (p *publisher) startSession(ctx context.Context, ds C.sr_datastore_t) (*C.sr_session_ctx_t, error) {
	conn, err := p.connection()
	if err != nil {
		return nil, err
	}
	var session *C.sr_session_ctx_t
	returnCode := C.sr_session_start(conn, ds, &session)
	if srFailed(returnCode) {
		startErr := srError("sr_session_start", returnCode)
		p.log.ErrorContext(ctx, "sysrepo session start failed", "err", startErr)
		return nil, startErr
	}
	return session, nil
}

// datastoreOf maps the package datastore names onto sysrepo's enum.
func datastoreOf(ds Datastore) (C.sr_datastore_t, error) {
	switch ds {
	case DatastoreRunning:
		return C.SR_DS_RUNNING, nil
	case DatastoreOperational:
		return C.SR_DS_OPERATIONAL, nil
	}
	return C.SR_DS_RUNNING, fmt.Errorf("yangpub: unknown datastore %q", string(ds))
}

// SetItems sets every item by path in ds and applies them as one change.
func (p *publisher) SetItems(ctx context.Context, ds Datastore, items []Item) error {
	return p.edit(ctx, ds, nil, items)
}

// ReplaceItems deletes deletePaths, sets items, and applies the edit as one
// change. Deletes are non-strict, so a path that holds nothing is a no-op
// rather than an error; that is what lets a publisher clear a subtree it
// may or may not have written on an earlier run.
func (p *publisher) ReplaceItems(ctx context.Context, ds Datastore, deletePaths []string, items []Item) error {
	return p.edit(ctx, ds, deletePaths, items)
}

// edit is the one write path: open a session on ds, stage every delete and
// then every set, and apply once. SetItems and ReplaceItems are the two
// public shapes of it.
func (p *publisher) edit(ctx context.Context, ds Datastore, deletePaths []string, items []Item) error {
	if err := ctx.Err(); err != nil {
		p.log.ErrorContext(ctx, "publish aborted before start", "err", err)
		return fmt.Errorf("yangpub: publish aborted: %w", err)
	}
	srDS, err := datastoreOf(ds)
	if err != nil {
		p.log.ErrorContext(ctx, "publish rejected", "err", err)
		return err
	}

	session, err := p.startSession(ctx, srDS)
	if err != nil {
		return err
	}
	defer C.sr_session_stop(session)

	for _, path := range deletePaths {
		cPath := C.CString(path)
		rc := C.sr_delete_item(session, cPath, C.sr_edit_options_t(0))
		C.free(unsafe.Pointer(cPath))
		if srFailed(rc) {
			deleteErr := srError("sr_delete_item "+path, rc)
			p.log.ErrorContext(ctx, "sysrepo delete failed", "path", path, "err", deleteErr)
			return deleteErr
		}
	}

	for _, item := range items {
		cPath := C.CString(item.Path)
		cValue := C.CString(item.Value)
		rc := C.sr_set_item_str(session, cPath, cValue, nil, C.sr_edit_options_t(0))
		C.free(unsafe.Pointer(cPath))
		C.free(unsafe.Pointer(cValue))
		if srFailed(rc) {
			setErr := srError("sr_set_item_str "+item.Path, rc)
			p.log.ErrorContext(ctx, "sysrepo set failed", "path", item.Path, "err", setErr)
			return setErr
		}
	}

	if rc := C.sr_apply_changes(session, C.uint32_t(applyTimeoutMS)); srFailed(rc) {
		applyErr := srError("sr_apply_changes", rc)
		p.log.ErrorContext(ctx, "sysrepo apply failed", "err", applyErr)
		return applyErr
	}
	return nil
}

// RegisterProvider registers fn as the operational provider of xpath
// inside module. The subscription and its session live until Close.
func (p *publisher) RegisterProvider(ctx context.Context, module string, xpath string, fn ProviderFunc) error {
	if err := ctx.Err(); err != nil {
		p.log.ErrorContext(ctx, "provider registration aborted before start", "err", err)
		return fmt.Errorf("yangpub: registration aborted: %w", err)
	}

	session, err := p.startSession(ctx, C.SR_DS_OPERATIONAL)
	if err != nil {
		return err
	}

	handle := cgo.NewHandle(&providerReg{log: p.log, fn: fn})
	handleCell := C.malloc(C.size_t(unsafe.Sizeof(C.uintptr_t(0))))
	*(*C.uintptr_t)(handleCell) = C.uintptr_t(handle)

	cModule := C.CString(module)
	cPath := C.CString(xpath)
	var subscription *C.sr_subscription_ctx_t
	rc := C.sr_oper_get_subscribe(session, cModule, cPath,
		C.sr_oper_get_items_cb(C.yangpubOperCB), handleCell,
		C.sr_subscr_options_t(0), &subscription)
	C.free(unsafe.Pointer(cModule))
	C.free(unsafe.Pointer(cPath))
	if srFailed(rc) {
		C.free(handleCell)
		handle.Delete()
		C.sr_session_stop(session)
		subscribeErr := srError("sr_oper_get_subscribe "+xpath, rc)
		p.log.ErrorContext(ctx, "sysrepo oper subscribe failed",
			"module", module, "path", xpath, "err", subscribeErr)
		return subscribeErr
	}

	p.mu.Lock()
	p.subscriptions = append(p.subscriptions, &operSubscription{
		session:      session,
		subscription: subscription,
		handle:       handle,
		handleCell:   handleCell,
	})
	p.mu.Unlock()
	return nil
}

// GetItem reads one value by path in ds.
func (p *publisher) GetItem(ctx context.Context, ds Datastore, path string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		p.log.ErrorContext(ctx, "read aborted before start", "err", err)
		return "", false, fmt.Errorf("yangpub: read aborted: %w", err)
	}
	srDS, err := datastoreOf(ds)
	if err != nil {
		p.log.ErrorContext(ctx, "read rejected", "err", err)
		return "", false, err
	}
	session, err := p.startSession(ctx, srDS)
	if err != nil {
		return "", false, err
	}
	defer C.sr_session_stop(session)

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var value *C.sr_val_t
	rc := C.sr_get_item(session, cPath, C.uint32_t(getTimeoutMS), &value)
	if rc == C.SR_ERR_NOT_FOUND {
		return "", false, nil
	}
	if srFailed(rc) {
		getErr := srError("sr_get_item "+path, rc)
		p.log.ErrorContext(ctx, "sysrepo get failed", "path", path, "err", getErr)
		return "", false, getErr
	}
	defer C.sr_free_val(value)

	printed := C.sr_val_to_str(value)
	if printed == nil {
		return "", false, fmt.Errorf("yangpub: sr_val_to_str %s returned no value", path)
	}
	defer C.free(unsafe.Pointer(printed))
	return C.GoString(printed), true, nil
}

// DeleteItem removes the value at path in ds and applies the change.
func (p *publisher) DeleteItem(ctx context.Context, ds Datastore, path string) error {
	if err := ctx.Err(); err != nil {
		p.log.ErrorContext(ctx, "delete aborted before start", "err", err)
		return fmt.Errorf("yangpub: delete aborted: %w", err)
	}
	srDS, err := datastoreOf(ds)
	if err != nil {
		p.log.ErrorContext(ctx, "delete rejected", "err", err)
		return err
	}
	session, err := p.startSession(ctx, srDS)
	if err != nil {
		return err
	}
	defer C.sr_session_stop(session)

	cPath := C.CString(path)
	rc := C.sr_delete_item(session, cPath, C.sr_edit_options_t(0))
	C.free(unsafe.Pointer(cPath))
	if srFailed(rc) {
		deleteErr := srError("sr_delete_item "+path, rc)
		p.log.ErrorContext(ctx, "sysrepo delete failed", "path", path, "err", deleteErr)
		return deleteErr
	}
	if rc := C.sr_apply_changes(session, C.uint32_t(applyTimeoutMS)); srFailed(rc) {
		applyErr := srError("sr_apply_changes", rc)
		p.log.ErrorContext(ctx, "sysrepo apply failed", "err", applyErr)
		return applyErr
	}
	return nil
}

// Close releases every registration and the connection. It is idempotent:
// a second call finds the connection already surrendered and returns nil
// rather than handing a freed pointer back to sysrepo.
func (p *publisher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	conn := p.conn
	p.conn = nil
	subs := p.subscriptions
	p.subscriptions = nil
	p.mu.Unlock()

	for _, sub := range subs {
		if rc := C.sr_unsubscribe(sub.subscription); srFailed(rc) {
			p.log.Error("sysrepo unsubscribe failed", "err", srError("sr_unsubscribe", rc))
		}
		C.sr_session_stop(sub.session)
		C.free(sub.handleCell)
		sub.handle.Delete()
	}
	if rc := C.sr_disconnect(conn); srFailed(rc) {
		disconnectErr := srError("sr_disconnect", rc)
		p.log.Error("sysrepo disconnect failed", "err", disconnectErr)
		return disconnectErr
	}
	return nil
}
