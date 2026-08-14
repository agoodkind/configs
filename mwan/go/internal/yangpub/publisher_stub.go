//go:build !linux || !cgo

package yangpub

import "log/slog"

// New reports the binding unavailable: this binary was built without cgo
// or for a platform sysrepo cannot run on. sysrepo requires robust
// pthread mutexes, which darwin does not provide, so linux is the only
// real-implementation platform. The caller is expected to continue
// without a management surface.
func New(log *slog.Logger) (Publisher, error) {
	log.Info("yangpub unavailable in this build")
	return nil, ErrUnavailable
}
