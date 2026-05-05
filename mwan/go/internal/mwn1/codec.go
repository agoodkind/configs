package mwn1

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

// MarshalRequest marshals req into wire bytes for the request side of
// methodID. It does not currently use the registry to enforce the
// concrete request type; the methodID is returned for the caller's
// convenience so that Send-site code reads symmetrically with
// UnmarshalRequest.
func MarshalRequest(reg *Registry, methodID uint16, req proto.Message) ([]byte, uint16, error) {
	if _, ok := reg.MethodName(methodID); !ok {
		return nil, 0, fmt.Errorf("mwn1: marshal request: unknown method id %d", methodID)
	}
	if req == nil {
		return nil, 0, fmt.Errorf("mwn1: marshal request: nil message for method id %d", methodID)
	}
	out, err := proto.Marshal(req)
	if err != nil {
		return nil, 0, fmt.Errorf("mwn1: marshal request id=%d: %w", methodID, err)
	}
	return out, methodID, nil
}

// UnmarshalRequest constructs a fresh request message for methodID
// via the registry and decodes payload into it. Returns an error if
// methodID is unknown or proto decoding fails.
func UnmarshalRequest(reg *Registry, methodID uint16, payload []byte) (proto.Message, error) {
	msg, ok := reg.NewRequest(methodID)
	if !ok {
		return nil, fmt.Errorf("mwn1: unmarshal request: unknown method id %d", methodID)
	}
	if err := proto.Unmarshal(payload, msg); err != nil {
		return nil, fmt.Errorf("mwn1: unmarshal request id=%d: %w", methodID, err)
	}
	return msg, nil
}

// MarshalResponse marshals resp into wire bytes for the response side
// of methodID.
func MarshalResponse(reg *Registry, methodID uint16, resp proto.Message) ([]byte, uint16, error) {
	if _, ok := reg.MethodName(methodID); !ok {
		return nil, 0, fmt.Errorf("mwn1: marshal response: unknown method id %d", methodID)
	}
	if resp == nil {
		return nil, 0, fmt.Errorf("mwn1: marshal response: nil message for method id %d", methodID)
	}
	out, err := proto.Marshal(resp)
	if err != nil {
		return nil, 0, fmt.Errorf("mwn1: marshal response id=%d: %w", methodID, err)
	}
	return out, methodID, nil
}

// UnmarshalResponse constructs a fresh response message for methodID
// via the registry and decodes payload into it.
func UnmarshalResponse(reg *Registry, methodID uint16, payload []byte) (proto.Message, error) {
	msg, ok := reg.NewResponse(methodID)
	if !ok {
		return nil, fmt.Errorf("mwn1: unmarshal response: unknown method id %d", methodID)
	}
	if err := proto.Unmarshal(payload, msg); err != nil {
		return nil, fmt.Errorf("mwn1: unmarshal response id=%d: %w", methodID, err)
	}
	return msg, nil
}
