//go:build linux

package main

import "goodkind.io/mwan/internal/yangpub"

func linkedSysrepoVersion() string {
	sysrepoVersion, err := yangpub.SysrepoVersion()
	if err != nil {
		return sysrepoVersionUnavailable
	}
	return sysrepoVersion
}
