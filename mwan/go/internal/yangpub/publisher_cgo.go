//go:build linux && cgo

package yangpub

/*
#cgo pkg-config: sysrepo libyang
#include <stdlib.h>
#include <sysrepo.h>
#include <libyang/libyang.h>

extern int yangpubOperCB(sr_session_ctx_t *session, uint32_t sub_id, const char *module_name,
        const char *path, const char *request_xpath, uint32_t operation_id,
        struct lyd_node **parent, void *private_data);
*/
import "C"

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/cgo"
	"sync"
	"unsafe"
)

// applyTimeoutMS bounds sr_apply_changes so a stuck datastore cannot
// stall the caller indefinitely.
const applyTimeoutMS = 5000

// publisher is the cgo-backed Publisher over one sysrepo connection.
type publisher struct {
	log  *slog.Logger
	conn *C.sr_conn_ctx_t

	mu            sync.Mutex
	subscriptions []*operSubscription
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

	var session *C.sr_session_ctx_t
	if rc := C.sr_session_start(p.conn, srDS, &session); rc != C.SR_ERR_OK {
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

	var session *C.sr_session_ctx_t
	if rc := C.sr_session_start(p.conn, C.SR_DS_OPERATIONAL, &session); rc != C.SR_ERR_OK {
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

// Close releases every registration and the connection.
func (p *publisher) Close() error {
	p.mu.Lock()
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
	if rc := C.sr_disconnect(p.conn); rc != C.SR_ERR_OK {
		p.log.Error("sysrepo disconnect failed", "detail", srErrorString(rc))
		return fmt.Errorf("yangpub: sr_disconnect: %s", srErrorString(rc))
	}
	return nil
}
