//go:build !linux

package main

// linkedSysrepoVersion is always unavailable off linux: the yangpub binding is
// linux-only, so this binary never links libsysrepo.
func linkedSysrepoVersion() string {
	return sysrepoVersionUnavailable
}
