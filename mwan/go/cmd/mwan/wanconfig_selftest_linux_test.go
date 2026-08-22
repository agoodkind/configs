//go:build linux && cgo

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// selftestModelSources are the gateway's model files as the repository
// carries them: the IETF modules and the interface-type registry from the
// models submodule, and the steering module from this repository.
var selftestModelSources = []string{
	"../../../../third_party/yang/standard/ietf/RFC/ietf-yang-types@2025-12-22.yang",
	"../../../../third_party/yang/standard/ietf/RFC/ietf-inet-types@2025-12-22.yang",
	"../../../../third_party/yang/standard/ietf/RFC/ietf-interfaces@2018-02-20.yang",
	"../../../../third_party/yang/standard/iana/iana-if-type@2026-03-17.yang",
	"../../../../third_party/yang/standard/ietf/RFC/ietf-ip@2018-02-22.yang",
	"../../../../third_party/yang/standard/ietf/RFC/ietf-nat@2019-01-10.yang",
	"../../../yang/goodkind-mwan-steering@2026-08-20.yang",
}

// TestWanconfigSelftest_PrivateRepository runs the private selftest the
// way an operator would, against a repository and model directory the
// test assembles. It is the end-to-end proof of the serving contract:
// with real libyang and sysrepo, one operational read over a second
// connection carries the published configuration and the provided live
// state together, and Close releases both. It is not parallel because
// sysrepo reads its repository location from the process environment.
func TestWanconfigSelftest_PrivateRepository(t *testing.T) {
	modelsDir := t.TempDir()
	for _, source := range selftestModelSources {
		absolute, err := filepath.Abs(source)
		if err != nil {
			t.Fatalf("resolve %s: %v", source, err)
		}
		if _, err := os.Stat(absolute); err != nil {
			t.Fatalf("model %s: %v", source, err)
		}
		if err := os.Symlink(absolute, filepath.Join(modelsDir, filepath.Base(source))); err != nil {
			t.Fatalf("link %s: %v", source, err)
		}
	}
	repository := filepath.Join(t.TempDir(), "repository")

	code := runWanconfigSelftest([]string{"--repository", repository, "--models-dir", modelsDir})
	if code != 0 {
		t.Fatalf("private selftest exit code = %d, want 0", code)
	}

	// The run's shared-memory segments carry this process's prefix and
	// must be gone, or every run leaves a set behind on the host.
	leftovers, err := filepath.Glob(filepath.Join(sysrepoSHMDir, fmt.Sprintf("mwanselftest%d*", os.Getpid())))
	if err != nil {
		t.Fatalf("list shared memory: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("shared memory left behind: %v", leftovers)
	}
}
