package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/goreleaser/nfpm/v2/files"
)

func testBuilder(t *testing.T) *builder {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &builder{log: log, pins: map[string]string{}, outDir: t.TempDir(), arch: "amd64", jobs: 1, versions: map[string]string{}}
}

// TestBuildDependsReadsTheUpstreamSysrepoTemplate pins that the upstream
// sysrepo v3.7.11 control file yields every Build-Depends package name,
// without version constraints, in template order.
func TestBuildDependsReadsTheUpstreamSysrepoTemplate(t *testing.T) {
	t.Parallel()
	names, err := testBuilder(t).buildDepends(context.Background(), filepath.Join("testdata", "sysrepo-control"))
	if err != nil {
		t.Fatalf("buildDepends: %v", err)
	}
	want := []string{"cmake", "debhelper", "libyang-dev", "libsystemd-dev", "pkg-config", "libcmocka-dev", "valgrind"}
	if !slices.Equal(names, want) {
		t.Fatalf("buildDepends = %v, want %v", names, want)
	}
}

// TestProjectVersionReadsNghttp2Asio pins the version derivation for the one
// component pinned to a commit.
func TestProjectVersionReadsNghttp2Asio(t *testing.T) {
	t.Parallel()
	version, ok := projectVersion("cmake_minimum_required(VERSION 3.0)\nproject(nghttp2-asio VERSION 0.0.90)\n")
	if !ok || version != "0.0.90" {
		t.Fatalf("projectVersion = %q, %v; want 0.0.90, true", version, ok)
	}
	if _, ok := projectVersion("project(rousette LANGUAGES CXX)\n"); ok {
		t.Fatal("projectVersion accepted a project() line without VERSION")
	}
}

// TestOwnerOfFollowsMergedUsrAliases pins that a library found under /lib
// resolves to the package that registered it under /usr/lib, which is how
// the dpkg database records files on a merged-/usr system.
func TestOwnerOfFollowsMergedUsrAliases(t *testing.T) {
	t.Parallel()
	owners := map[string]string{"/usr/lib/x86_64-linux-gnu/libyang.so.3.9.14": "libyang3"}
	pkg, ok := ownerOf("/lib/x86_64-linux-gnu/libyang.so.3.9.14", owners)
	if !ok || pkg != "libyang3" {
		t.Fatalf("ownerOf = %q, %v; want libyang3, true", pkg, ok)
	}
	if _, ok := ownerOf("/lib/x86_64-linux-gnu/libnone.so.1", owners); ok {
		t.Fatal("ownerOf accepted a path no package owns")
	}
}

// TestStageContentsKeepsSymlinksAndMarksLoaderConf pins that a staged cmake
// install becomes package contents with directories, symlinks preserved as
// symlinks, and the loader conf typed as configuration and naming the
// staged library directory.
func TestStageContentsKeepsSymlinksAndMarksLoaderConf(t *testing.T) {
	t.Parallel()
	stage := t.TempDir()
	libDir := filepath.Join(stage, stackPrefix, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "libyang-cpp.so.4"), []byte("elf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("libyang-cpp.so.4", filepath.Join(libDir, "libyang-cpp.so")); err != nil {
		t.Fatal(err)
	}
	if err := testBuilder(t).writeLoaderConf(context.Background(), stage); err != nil {
		t.Fatalf("writeLoaderConf: %v", err)
	}
	contents, err := testBuilder(t).stageContents(context.Background(), stage)
	if err != nil {
		t.Fatalf("stageContents: %v", err)
	}
	byDestination := map[string]*files.Content{}
	for _, content := range contents {
		byDestination[content.Destination] = content
	}
	link := byDestination[stackPrefix+"/lib/libyang-cpp.so"]
	if link == nil || link.Type != files.TypeSymlink || link.Source != "libyang-cpp.so.4" {
		t.Fatalf("symlink content = %+v", link)
	}
	if dir := byDestination[stackPrefix+"/lib"]; dir == nil || dir.Type != files.TypeDir {
		t.Fatalf("dir content = %+v", dir)
	}
	conf := byDestination[loaderConfPath]
	if conf == nil || conf.Type != files.TypeConfig {
		t.Fatalf("loader conf content = %+v", conf)
	}
	text, err := os.ReadFile(conf.Source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(text)) != stackPrefix+"/lib" {
		t.Fatalf("loader conf = %q, want %s/lib", text, stackPrefix)
	}
}

// TestWriteBundleRoundTrips pins the bundle layout a consumer unpacks: the
// manifest first, then each package under debs/, with the manifest naming
// the package, version, architecture, sha256, and member path.
func TestWriteBundleRoundTrips(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	debPath := filepath.Join(dir, "libyang3_3.13.6-1_amd64.deb")
	if err := os.WriteFile(debPath, []byte("deb bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := testBuilder(t)
	sum, err := b.fileSHA256(context.Background(), debPath)
	if err != nil {
		t.Fatal(err)
	}
	members := []bundleMember{{name: "libyang3", version: "3.13.6-1", path: debPath, sha256: sum}}
	var archive bytes.Buffer
	if err := b.writeBundle(context.Background(), &archive, manifest("amd64", members), members); err != nil {
		t.Fatalf("writeBundle: %v", err)
	}
	gz, err := gzip.NewReader(&archive)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	got := map[string]string{}
	order := []string{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		got[header.Name] = string(content)
		order = append(order, header.Name)
	}
	if !slices.Equal(order, []string{manifestName, "debs/libyang3_3.13.6-1_amd64.deb"}) {
		t.Fatalf("members = %v", order)
	}
	if got["debs/libyang3_3.13.6-1_amd64.deb"] != "deb bytes" {
		t.Fatalf("package member = %q", got["debs/libyang3_3.13.6-1_amd64.deb"])
	}
	wantLine := "libyang3 3.13.6-1 amd64 " + sum + " debs/libyang3_3.13.6-1_amd64.deb"
	if !strings.Contains(got[manifestName], wantLine) {
		t.Fatalf("manifest = %q, want line %q", got[manifestName], wantLine)
	}
}

// TestParseFlagsRequiresEveryPin pins that a missing pin or output directory
// refuses to run rather than building with a blank version.
func TestParseFlagsRequiresEveryPin(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := parseFlags(log, []string{"-out", "/tmp/x"}); err == nil {
		t.Fatal("parseFlags accepted missing pins")
	}
	args := []string{"-out", "/tmp/x"}
	for _, c := range components {
		args = append(args, "-"+c.pinVar, "v1")
	}
	opts, err := parseFlags(log, args)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if opts.outDir != "/tmp/x" || len(opts.pins) != len(components) {
		t.Fatalf("parseFlags = %+v", opts)
	}
	verify, err := parseFlags(log, []string{"-verify", "/out/wanconfig-stack_linux_amd64.tar.gz"})
	if err != nil {
		t.Fatalf("parseFlags verify: %v", err)
	}
	if verify.verifyBundle != "/out/wanconfig-stack_linux_amd64.tar.gz" {
		t.Fatalf("parseFlags verify = %+v", verify)
	}
}
