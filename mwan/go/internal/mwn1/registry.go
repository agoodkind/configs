package mwn1

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
)

// Method-id assignment policy.
//
// Method ids are stable, manually-assigned uint16 values. Once an id
// is shipped to production, it must never be reused for a different
// RPC, even after the original RPC is retired. This keeps the wire
// format compatible across mixed-version daemon/bridge pairs that
// may coexist briefly during a re-exec deploy.
//
// New RPCs take the next free id. Id 0 is reserved.
//
// The mapping is registered manually rather than generated, because
// a code generator would couple wire stability to proto compilation
// order, which protoc does not guarantee. See registry_mwan_opnsense.go
// for the canonical mwan_opnsense assignments.

// MessageFactory returns a fresh, zero-valued proto.Message of a
// concrete type. Used by the codec to allocate decode targets.
type MessageFactory func() proto.Message

// methodEntry captures the per-method registration data.
type methodEntry struct {
	id          uint16
	name        string
	newRequest  MessageFactory
	newResponse MessageFactory
}

// Registry maps proto method names (e.g. "mwan.v1.MWANOPNsenseService/Version")
// to stable wire method ids and back. Registry instances are
// goroutine-safe for read after construction; concurrent Register
// calls are not supported.
type Registry struct {
	byID   map[uint16]methodEntry
	byName map[string]methodEntry
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byID:   make(map[uint16]methodEntry),
		byName: make(map[string]methodEntry),
	}
}

// Register associates name with id and the request/response factories.
// Returns an error if id or name is already registered, or if id is
// zero (reserved). The factories must return non-nil messages.
func (r *Registry) Register(id uint16, name string, newRequest, newResponse MessageFactory) error {
	if id == 0 {
		return errors.New("mwn1: method id 0 is reserved")
	}
	if name == "" {
		return errors.New("mwn1: method name is empty")
	}
	if newRequest == nil || newResponse == nil {
		return fmt.Errorf("mwn1: method %q: nil factory", name)
	}
	if existing, ok := r.byID[id]; ok {
		return fmt.Errorf("mwn1: method id %d already registered as %q", id, existing.name)
	}
	if existing, ok := r.byName[name]; ok {
		return fmt.Errorf("mwn1: method %q already registered with id %d", name, existing.id)
	}
	entry := methodEntry{
		id:          id,
		name:        name,
		newRequest:  newRequest,
		newResponse: newResponse,
	}
	r.byID[id] = entry
	r.byName[name] = entry
	return nil
}

// MethodID looks up the wire id for a method name.
func (r *Registry) MethodID(name string) (uint16, bool) {
	e, ok := r.byName[name]
	if !ok {
		return 0, false
	}
	return e.id, true
}

// MethodName looks up the method name for a wire id.
func (r *Registry) MethodName(id uint16) (string, bool) {
	e, ok := r.byID[id]
	if !ok {
		return "", false
	}
	return e.name, true
}

// NewRequest constructs a fresh request message for the given id.
// Returns false if id is not registered.
func (r *Registry) NewRequest(id uint16) (proto.Message, bool) {
	e, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	return e.newRequest(), true
}

// NewResponse constructs a fresh response message for the given id.
// Returns false if id is not registered.
func (r *Registry) NewResponse(id uint16) (proto.Message, bool) {
	e, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	return e.newResponse(), true
}
