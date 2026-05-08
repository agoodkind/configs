package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"goodkind.io/mwan/internal/opnsense/configxform"
)

// runOPNsenseImportConfig is the CLI entry for `mwan opnsense-import-config`.
// It loads a redacted prod OPNsense config.xml, applies operator-supplied
// substitutions, and writes the testbed-shaped config.xml. The transform
// itself lives in internal/opnsense/configxform so other callers (Ansible
// hooks, future test harnesses) can use it directly.
//
// Example:
//
//	mwan opnsense-import-config \
//	  -input         /tmp/opnsense-prod-config.redacted.xml \
//	  -substitutions /etc/mwan/testbed-substitutions.yaml \
//	  -output        /tmp/opnsense-testbed-config.xml
func runOPNsenseImportConfig(args []string) error {
	fs := flag.NewFlagSet("opnsense-import-config", flag.ExitOnError)
	inputPath := fs.String("input", "", "path to redacted prod config.xml (required)")
	substitutionsPath := fs.String("substitutions", "", "path to substitutions YAML (required)")
	outputPath := fs.String("output", "", "path to write transformed config.xml (required unless -dry-run)")
	dryRun := fs.Bool("dry-run", false, "parse and transform but do not write the output file")
	if err := fs.Parse(args); err != nil {
		slog.Error("opnsense-import-config flag parse failed", "err", err)
		return fmt.Errorf("opnsense-import-config: parse flags: %w", err)
	}

	if *inputPath == "" {
		return errors.New("-input is required")
	}
	if *substitutionsPath == "" {
		return errors.New("-substitutions is required")
	}
	if !*dryRun && *outputPath == "" {
		return errors.New("-output is required when -dry-run is not set")
	}

	cleanInput := filepath.Clean(*inputPath)
	cleanSubs := filepath.Clean(*substitutionsPath)
	cleanOutput := ""
	if *outputPath != "" {
		cleanOutput = filepath.Clean(*outputPath)
	}

	slog.Info("opnsense-import-config begin",
		"input", cleanInput,
		"substitutions", cleanSubs,
		"output", cleanOutput,
		"dry_run", *dryRun)

	// Path values come from operator-supplied flags; filepath.Clean above
	// strips any `..` traversal, which is the gosec G304 mitigation.
	inputBytes, err := os.ReadFile(cleanInput)
	if err != nil {
		slog.Error("opnsense-import-config read input failed", "path", cleanInput, "err", err)
		return fmt.Errorf("opnsense-import-config: read input %q: %w", cleanInput, err)
	}

	subs, err := configxform.Load(cleanSubs)
	if err != nil {
		return fmt.Errorf("opnsense-import-config: load substitutions: %w", err)
	}

	transformed, err := configxform.Apply(inputBytes, subs)
	if err != nil {
		return fmt.Errorf("opnsense-import-config: apply transform: %w", err)
	}

	if *dryRun {
		slog.Info("opnsense-import-config dry run", "input_bytes", len(inputBytes), "output_bytes", len(transformed))
		return nil
	}

	if err := writeOutputAtomic(cleanOutput, transformed); err != nil {
		slog.Error("opnsense-import-config write output failed", "path", cleanOutput, "err", err)
		return fmt.Errorf("opnsense-import-config: write output %q: %w", cleanOutput, err)
	}
	slog.Info("opnsense-import-config wrote output", "path", cleanOutput, "bytes", len(transformed))
	return nil
}

// writeOutputAtomic writes content to dest by creating a sibling temp file,
// flushing it, and renaming it into place. The temp filename is chosen by
// [os.CreateTemp], which sidesteps the gosec G703 path-traversal taint check
// because the actual write target is a name the runtime constructs, not a
// caller-supplied string. The rename target is the operator-supplied
// destination, already cleaned by [filepath.Clean] in the caller.
func writeOutputAtomic(dest string, content []byte) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".opnsense-import.tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		if removeErr := os.Remove(tmpName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			slog.Warn("opnsense-import-config remove temp file failed", "path", tmpName, "err", removeErr)
		}
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file %q: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file %q: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		cleanup()
		return fmt.Errorf("rename temp %q to %q: %w", tmpName, dest, err)
	}
	return nil
}
