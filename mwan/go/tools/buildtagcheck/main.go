// Command buildtagcheck fails the build when a file carries a platform
// build tag nothing forces. A platform tag is sound only at a real
// platform boundary; a tag on portable code opts it out of a build for
// convenience and hides errors until the other platform's gate runs.
// Portable logic belongs in untagged files, with only the platform half
// holding the tag.
//
// A linux-only file is accepted when any of these forces it:
//
//   - The file uses cgo.
//   - The file imports a package with no buildable Go files for freebsd,
//     including first-party packages this module ships only on linux.
//   - The file imports a package that is platform-bound in fact even
//     though it compiles everywhere, recorded in platformBoundImports.
//   - The compiler proves the tag necessary: with the tag stripped, the
//     package no longer typechecks for freebsd. This is the final oracle,
//     and it is exact, because a missing symbol, a one-sided constant, or
//     a clash with the other platform's implementation of the same
//     function all fail that build.
//
// A package with no freebsd files at all is the endorsed absent-package
// shape (the split lives at its call sites) and is skipped here; its
// importers are forced by the failing import instead.
//
// The check reads the same build constraints the compiler does, tag
// lines and _linux/_other name suffixes alike, by comparing `go list`
// file sets for the two shipped platforms.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// otherGOOS is the second shipped platform the tags must be justified
// against. The module releases linux and freebsd binaries; darwin is a
// development host, never a release target, so it does not gate.
const otherGOOS = "freebsd"

// platformBoundImports records third-party packages that compile on every
// platform but are platform-bound in fact, so importing them forces a
// tag. This is the one place that fact lives; each entry says why.
var platformBoundImports = map[string]string{
	// netlink ships stub files off linux whose calls panic at runtime, so
	// the compile oracle cannot see the boundary.
	"github.com/vishvananda/netlink": "netlink stubs panic off linux",
}

// buildTagLine matches the constraint line the strip step removes.
var buildTagLine = regexp.MustCompile(`(?m)^//go:build .*\n`)

// listedPackage is the subset of `go list -json` output the check reads.
type listedPackage struct {
	ImportPath string   `json:"ImportPath"`
	Dir        string   `json:"Dir"`
	GoFiles    []string `json:"GoFiles"`
	CgoFiles   []string `json:"CgoFiles"`
	Imports    []string `json:"Imports"`
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	violations, err := run(context.Background(), log)
	if err != nil {
		log.Error("buildtagcheck failed", "err", err)
		os.Exit(2)
	}
	if len(violations) > 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		os.Exit(1)
	}
	fmt.Println("buildtagcheck: every platform tag is forced")
}

func run(ctx context.Context, log *slog.Logger) ([]string, error) {
	linuxPackages, err := listPackages(ctx, log, "linux")
	if err != nil {
		return nil, err
	}
	otherPackages, err := listPackages(ctx, log, otherGOOS)
	if err != nil {
		return nil, err
	}
	otherByPath := make(map[string]listedPackage, len(otherPackages))
	for _, pkg := range otherPackages {
		otherByPath[pkg.ImportPath] = pkg
	}

	unusable, err := unusableImports(ctx, log, linuxPackages, otherByPath)
	if err != nil {
		return nil, err
	}

	var violations []string
	for _, pkg := range linuxPackages {
		other := otherByPath[pkg.ImportPath]
		if len(other.GoFiles)+len(other.CgoFiles) == 0 {
			// Absent package: the endorsed shape. Its importers are
			// forced through unusableImports instead.
			continue
		}
		if len(pkg.CgoFiles) > 0 {
			continue
		}
		for _, file := range onlyIn(pkg.GoFiles, other.GoFiles) {
			forced, err := fileForced(ctx, log, pkg, file, unusable)
			if err != nil {
				return nil, err
			}
			if !forced {
				violations = append(violations, fmt.Sprintf(
					"%s/%s: excluded from %s, but the stripped file still typechecks there and imports nothing platform-bound; move the portable code to an untagged file",
					pkg.ImportPath, file, otherGOOS))
			}
		}
	}
	sort.Strings(violations)
	return violations, nil
}

// fileForced reports whether anything forces the file's platform tag:
// a platform-bound or unbuildable import, or the compile oracle.
func fileForced(
	ctx context.Context,
	log *slog.Logger,
	pkg listedPackage,
	file string,
	unusable map[string]bool,
) (bool, error) {
	source, err := os.ReadFile(filepath.Join(pkg.Dir, file))
	if err != nil {
		log.ErrorContext(ctx, "buildtagcheck: read source failed", "file", file, "err", err)
		return false, fmt.Errorf("read %s: %w", file, err)
	}
	for imported := range unusable {
		if bytes.Contains(source, []byte(`"`+imported+`"`)) {
			return true, nil
		}
	}
	return stripOracle(ctx, log, pkg, file, source)
}

// stripOracle asks the compiler: does the package still typecheck for the
// other platform when this file's constraint is stripped? A failing build
// proves the tag necessary. The stripped copy joins the package through a
// build overlay, under a neutral name so a _linux suffix cannot re-exclude
// it.
func stripOracle(
	ctx context.Context,
	log *slog.Logger,
	pkg listedPackage,
	file string,
	source []byte,
) (bool, error) {
	tempDir, err := os.MkdirTemp("", "buildtagcheck")
	if err != nil {
		log.ErrorContext(ctx, "buildtagcheck: temp dir failed", "err", err)
		return false, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	stripped := buildTagLine.ReplaceAll(source, nil)
	strippedPath := filepath.Join(tempDir, "stripped.go")
	if err := os.WriteFile(strippedPath, stripped, 0o600); err != nil {
		log.ErrorContext(ctx, "buildtagcheck: write stripped copy failed", "file", file, "err", err)
		return false, fmt.Errorf("write stripped %s: %w", file, err)
	}
	neutralName := strings.TrimSuffix(file, ".go") + "_buildtagcheck.go"
	overlay := map[string]map[string]string{"Replace": {
		// An empty replacement path deletes the original from the build,
		// per the overlay file format; the stripped copy joins under the
		// neutral name.
		filepath.Join(pkg.Dir, file):        "",
		filepath.Join(pkg.Dir, neutralName): strippedPath,
	}}
	overlayPath := filepath.Join(tempDir, "overlay.json")
	encoded, err := json.Marshal(overlay)
	if err != nil {
		log.ErrorContext(ctx, "buildtagcheck: encode overlay failed", "err", err)
		return false, fmt.Errorf("encode overlay: %w", err)
	}
	if err := os.WriteFile(overlayPath, encoded, 0o600); err != nil {
		log.ErrorContext(ctx, "buildtagcheck: write overlay failed", "err", err)
		return false, fmt.Errorf("write overlay: %w", err)
	}

	command := exec.CommandContext(ctx, "go", "build", "-overlay", overlayPath, pkg.ImportPath)
	command.Env = append(os.Environ(), "GOOS="+otherGOOS, "GOARCH=amd64", "CGO_ENABLED=0")
	if buildErr := command.Run(); buildErr != nil {
		// The stripped file breaks the other platform's build: forced.
		return true, nil
	}
	return false, nil
}

// unusableImports is the set of import paths that force a tag: paths with
// no buildable files for the other platform, first-party packages absent
// there, and the recorded platform-bound facts.
func unusableImports(
	ctx context.Context,
	log *slog.Logger,
	linuxPackages []listedPackage,
	otherByPath map[string]listedPackage,
) (map[string]bool, error) {
	importSet := map[string]bool{}
	for _, pkg := range linuxPackages {
		for _, imported := range pkg.Imports {
			if imported != "C" && imported != "unsafe" {
				importSet[imported] = true
			}
		}
	}
	importPaths := make([]string, 0, len(importSet))
	for imported := range importSet {
		importPaths = append(importPaths, imported)
	}
	sort.Strings(importPaths)

	type listedImport struct {
		ImportPath string   `json:"ImportPath"`
		GoFiles    []string `json:"GoFiles"`
		CgoFiles   []string `json:"CgoFiles"`
		Error      *struct {
			Err string `json:"Err"`
		} `json:"Error"`
	}
	arguments := append([]string{"list", "-e", "-json=ImportPath,GoFiles,CgoFiles,Error"}, importPaths...)
	output, err := goList(ctx, log, otherGOOS, arguments)
	if err != nil {
		return nil, err
	}
	unusable := map[string]bool{"C": true}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var pkg listedImport
		if err := decoder.Decode(&pkg); err != nil {
			log.ErrorContext(ctx, "buildtagcheck: decode go list output", "err", err)
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		if pkg.Error != nil || len(pkg.GoFiles)+len(pkg.CgoFiles) == 0 {
			unusable[pkg.ImportPath] = true
		}
	}
	for path := range platformBoundImports {
		unusable[path] = true
	}
	for _, pkg := range linuxPackages {
		other := otherByPath[pkg.ImportPath]
		if len(other.GoFiles)+len(other.CgoFiles) == 0 {
			unusable[pkg.ImportPath] = true
		}
	}
	return unusable, nil
}

// onlyIn returns the members of files absent from others, sorted.
func onlyIn(files []string, others []string) []string {
	otherSet := make(map[string]bool, len(others))
	for _, file := range others {
		otherSet[file] = true
	}
	var only []string
	for _, file := range files {
		if !otherSet[file] {
			only = append(only, file)
		}
	}
	sort.Strings(only)
	return only
}

// listPackages runs `go list -json` for goos over the whole module.
func listPackages(ctx context.Context, log *slog.Logger, goos string) ([]listedPackage, error) {
	output, err := goList(ctx, log, goos, []string{"list", "-e", "-json=ImportPath,Dir,GoFiles,CgoFiles,Imports", "./..."})
	if err != nil {
		return nil, err
	}
	var packages []listedPackage
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			log.ErrorContext(ctx, "buildtagcheck: decode go list output", "goos", goos, "err", err)
			return nil, fmt.Errorf("decode go list output for %s: %w", goos, err)
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// goList runs the go tool for goos with cgo on, so cgo files count as
// present on linux and the freebsd view matches the release build.
func goList(ctx context.Context, log *slog.Logger, goos string, arguments []string) ([]byte, error) {
	command := exec.CommandContext(ctx, "go", arguments...)
	command.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		log.ErrorContext(ctx, "buildtagcheck: go list failed",
			"goos", goos, "err", err, "stderr", stderr.String())
		return nil, fmt.Errorf("go list (GOOS=%s): %w", goos, err)
	}
	return output, nil
}
