//go:build linux && cgo

package yangpub

/*
#include <stdlib.h>
#include <sysrepo.h>
#include <libyang/libyang.h>
*/
import "C"

import (
	"context"
	"fmt"
	"runtime/cgo"
	"time"
	"unsafe"
)

// providerTimeout bounds one provider call. A provider that blocks past
// it loses its sysrepo worker thread to a cancelled context rather than
// holding it for the life of the read.
const providerTimeout = 5 * time.Second

// operGetCallback holds the trampoline sysrepo invokes. The only real
// caller is C, through the exported symbol, and no Go analysis can see
// that edge; this reference states it so reachability analysis agrees
// with what the program does.
var operGetCallback = yangpubOperCB

// callProvider runs one provider with a deadline and turns a panic into
// an error. The panic case is the load-bearing one: this runs on a
// thread that entered Go from C, and a panic that unwinds past this
// frame aborts the whole process, so one bad provider would take the
// daemon down with it.
func callProvider(registration *providerReg, xpath string) (items []Item, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("yangpub: provider panicked: %v", recovered)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), providerTimeout)
	defer cancel()
	return registration.fn(ctx, xpath)
}

// yangpubOperCB is the C-visible trampoline sysrepo invokes for every
// operational read under a registered provider. It recovers the Go
// registration through the handle cell in private_data, asks the
// provider for its items, and grafts them onto the reply tree with
// libyang. Any provider or tree error fails only this read; the daemon
// and every other registration keep running.
//
//export yangpubOperCB
func yangpubOperCB(session *C.sr_session_ctx_t, subID C.uint32_t, moduleName *C.char,
	path *C.char, requestXPath *C.char, operationID C.uint32_t,
	parent **C.struct_lyd_node, privateData unsafe.Pointer,
) C.int {
	handle := cgo.Handle(*(*C.uintptr_t)(privateData))
	registration, ok := handle.Value().(*providerReg)
	if !ok {
		return C.SR_ERR_INTERNAL
	}

	requested := C.GoString(path)
	if requestXPath != nil && *requestXPath != 0 {
		requested = C.GoString(requestXPath)
	}
	items, err := callProvider(registration, requested)
	if err != nil {
		registration.log.Error("provider callback failed",
			"xpath", requested, "err", err)
		return C.SR_ERR_CALLBACK_FAILED
	}

	connection := C.sr_session_get_connection(session)
	lyContext := C.sr_acquire_context(connection)
	defer C.sr_release_context(connection)

	for _, item := range items {
		cPath := C.CString(item.Path)
		cValue := C.CString(item.Value)
		var lyErr C.LY_ERR
		if *parent == nil {
			lyErr = C.lyd_new_path(nil, lyContext, cPath, cValue, 0, parent)
		} else {
			lyErr = C.lyd_new_path(*parent, nil, cPath, cValue, 0, nil)
		}
		C.free(unsafe.Pointer(cPath))
		C.free(unsafe.Pointer(cValue))
		if lyErr != C.LY_SUCCESS {
			registration.log.Error("provider tree build failed",
				"path", item.Path, "code", int(lyErr))
			return C.SR_ERR_CALLBACK_FAILED
		}
	}
	return C.SR_ERR_OK
}
