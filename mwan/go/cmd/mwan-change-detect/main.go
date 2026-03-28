// mwan-change-detect computes a composite SHA-256 over critical MWAN VM config
// files and writes /var/run/mwan-config-hash plus /var/run/mwan-last-change.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

func criticalPaths() []string {
	var paths []string
	add := func(p string) {
		paths = append(paths, p)
	}
	add("/etc/mwan/mwan.env")
	add("/etc/nftables.conf")
	add("/etc/iproute2/rt_tables")
	add("/etc/sysctl.d/99-mwan.conf")
	add("/etc/wpa_supplicant/wpa_supplicant.conf")
	for _, g := range []string{
		"/etc/systemd/network/*",
		"/etc/networkd-dispatcher/routable.d/*",
	} {
		matches, err := filepath.Glob(g)
		if err != nil {
			continue
		}
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil || st.IsDir() {
				continue
			}
			add(m)
		}
	}
	sort.Strings(paths)
	return paths
}

func main() {
	h := sha256.New()
	for _, p := range criticalPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
	}
	sum := h.Sum(nil)
	hashHex := hex.EncodeToString(sum)
	if err := os.WriteFile(
		"/var/run/mwan-config-hash",
		[]byte(hashHex+"\n"),
		0o644,
	); err != nil {
		fmt.Fprintf(os.Stderr, "mwan-change-detect: write hash: %v\n", err)
		os.Exit(1)
	}
	epoch := strconv.FormatInt(time.Now().Unix(), 10)
	if err := os.WriteFile(
		"/var/run/mwan-last-change",
		[]byte(epoch+"\n"),
		0o644,
	); err != nil {
		fmt.Fprintf(os.Stderr, "mwan-change-detect: write change marker: %v\n", err)
		os.Exit(1)
	}
}
