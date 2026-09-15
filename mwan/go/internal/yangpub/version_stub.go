//go:build linux && !cgo

package yangpub

// SysrepoVersion reports the binding unavailable: this linux binary was built
// with cgo off, so libsysrepo is not linked.
func SysrepoVersion() (string, error) {
	return "", ErrUnavailable
}
