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
	if rc := C.sr_connect(C.sr_conn_options_t(0), &conn); rc != C.SR_ERR_OK {
		log.Error("sysrepo connect failed", "detail", srErrorString(rc))
		return nil, fmt.Errorf("yangpub: sr_connect: %s", srErrorString(rc))
	}
	return &publisher{log: log, conn: conn}, nil
}

// srErrorString renders a sysrepo return code as its message.
func srErrorString(returnCode C.int) string {
	return C.GoString(C.sr_strerror(returnCode))
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
	if err := ctx.Err(); err != nil {
		p.log.Error("publish aborted before start", "err", err)
		return fmt.Errorf("yangpub: publish aborted: %w", err)
	}
	srDS, err := datastoreOf(ds)
	if err != nil {
		p.log.Error("publish rejected", "err", err)
		return err
	}

	conn, err := p.connection()
	if err != nil {
		return err
	}

	var session *C.sr_session_ctx_t
	if rc := C.sr_session_start(conn, srDS, &session); rc != C.SR_ERR_OK {
		p.log.Error("sysrepo session start failed", "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_session_start: %s", srErrorString(rc))
	}
	defer C.sr_session_stop(session)

	for _, item := range items {
		cPath := C.CString(item.Path)
		cValue := C.CString(item.Value)
		rc := C.sr_set_item_str(session, cPath, cValue, nil, C.sr_edit_options_t(0))
		C.free(unsafe.Pointer(cPath))
		C.free(unsafe.Pointer(cValue))
		if rc != C.SR_ERR_OK {
			p.log.Error("sysrepo set failed",
				"path", item.Path, "detail", srErrorString(rc))
			return fmt.Errorf("yangpub: sr_set_item_str %s: %s", item.Path, srErrorString(rc))
		}
	}

	if rc := C.sr_apply_changes(session, C.uint32_t(applyTimeoutMS)); rc != C.SR_ERR_OK {
		p.log.Error("sysrepo apply failed", "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_apply_changes: %s", srErrorString(rc))
	}
	return nil
}

// RegisterProvider registers fn as the operational provider of xpath
// inside module. The subscription and its session live until Close.
func (p *publisher) RegisterProvider(ctx context.Context, module string, xpath string, fn ProviderFunc) error {
	if err := ctx.Err(); err != nil {
		p.log.Error("provider registration aborted before start", "err", err)
		return fmt.Errorf("yangpub: registration aborted: %w", err)
	}

	conn, err := p.connection()
	if err != nil {
		return err
	}

	var session *C.sr_session_ctx_t
	if rc := C.sr_session_start(conn, C.SR_DS_OPERATIONAL, &session); rc != C.SR_ERR_OK {
		p.log.Error("sysrepo session start failed", "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_session_start: %s", srErrorString(rc))
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
	if rc != C.SR_ERR_OK {
		C.free(handleCell)
		handle.Delete()
		C.sr_session_stop(session)
		p.log.Error("sysrepo oper subscribe failed",
			"module", module, "path", xpath, "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_oper_get_subscribe %s: %s", xpath, srErrorString(rc))
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
		p.log.Error("read aborted before start", "err", err)
		return "", false, fmt.Errorf("yangpub: read aborted: %w", err)
	}
	srDS, err := datastoreOf(ds)
	if err != nil {
		p.log.Error("read rejected", "err", err)
		return "", false, err
	}
	conn, err := p.connection()
	if err != nil {
		return "", false, err
	}

	var session *C.sr_session_ctx_t
	if rc := C.sr_session_start(conn, srDS, &session); rc != C.SR_ERR_OK {
		p.log.Error("sysrepo session start failed", "detail", srErrorString(rc))
		return "", false, fmt.Errorf("yangpub: sr_session_start: %s", srErrorString(rc))
	}
	defer C.sr_session_stop(session)

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var value *C.sr_val_t
	rc := C.sr_get_item(session, cPath, C.uint32_t(getTimeoutMS), &value)
	if rc == C.SR_ERR_NOT_FOUND {
		return "", false, nil
	}
	if rc != C.SR_ERR_OK {
		p.log.Error("sysrepo get failed", "path", path, "detail", srErrorString(rc))
		return "", false, fmt.Errorf("yangpub: sr_get_item %s: %s", path, srErrorString(rc))
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
		p.log.Error("delete aborted before start", "err", err)
		return fmt.Errorf("yangpub: delete aborted: %w", err)
	}
	srDS, err := datastoreOf(ds)
	if err != nil {
		p.log.Error("delete rejected", "err", err)
		return err
	}
	conn, err := p.connection()
	if err != nil {
		return err
	}

	var session *C.sr_session_ctx_t
	if rc := C.sr_session_start(conn, srDS, &session); rc != C.SR_ERR_OK {
		p.log.Error("sysrepo session start failed", "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_session_start: %s", srErrorString(rc))
	}
	defer C.sr_session_stop(session)

	cPath := C.CString(path)
	rc := C.sr_delete_item(session, cPath, C.sr_edit_options_t(0))
	C.free(unsafe.Pointer(cPath))
	if rc != C.SR_ERR_OK {
		p.log.Error("sysrepo delete failed", "path", path, "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_delete_item %s: %s", path, srErrorString(rc))
	}
	if rc := C.sr_apply_changes(session, C.uint32_t(applyTimeoutMS)); rc != C.SR_ERR_OK {
		p.log.Error("sysrepo apply failed", "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_apply_changes: %s", srErrorString(rc))
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
		if rc := C.sr_unsubscribe(sub.subscription); rc != C.SR_ERR_OK {
			p.log.Error("sysrepo unsubscribe failed", "detail", srErrorString(rc))
		}
		C.sr_session_stop(sub.session)
		C.free(sub.handleCell)
		sub.handle.Delete()
	}
	if rc := C.sr_disconnect(conn); rc != C.SR_ERR_OK {
		p.log.Error("sysrepo disconnect failed", "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_disconnect: %s", srErrorString(rc))
	}
	return nil
}
