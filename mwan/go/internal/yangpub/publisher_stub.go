//go:build linux && !cgo

package yangpub

import (
	"log/slog"
)

// New reports the binding unavailable: this linux binary was built with cgo
// off, so libsysrepo is not linked. It keeps a development build compiling
// and tells the operator why it cannot publish. The release guard refuses to
// ship this variant, so a deployed linux binary never reaches here.
func New(log *slog.Logger) (Publisher, error) {
	log.Info("yangpub unavailable in this build")
	return nil, ErrUnavailable
}
