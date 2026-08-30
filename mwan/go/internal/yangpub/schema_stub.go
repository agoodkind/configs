//go:build linux && !cgo

package yangpub

// Schema is the handle a cgo-less build cannot provide. It exists so the
// package's callers compile in a development build; the release guard refuses
// to ship this variant.
type Schema struct{}

// LoadSchema reports the binding unavailable: this linux binary was built with
// cgo off, so libyang is not linked.
func LoadSchema(_ string) (*Schema, error) {
	return nil, ErrUnavailable
}

// ValidateConfigJSON reports the binding unavailable, so a caller never treats
// an unvalidated file as validated.
func (s *Schema) ValidateConfigJSON(_ []byte) error {
	return ErrUnavailable
}

// Close is a no-op: a stub holds no context.
func (s *Schema) Close() {}
