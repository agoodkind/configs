// Package main wraps the pinned upstream govulncheck scanner and suppresses
// one known false positive for gobgp until the vulnerability metadata catches
// up with the fixed release.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	gobgpModule                = "github.com/osrg/gobgp/v4"
	knownGobgpFixedVersion     = "v4.3.0"
	knownGobgpVulnID           = "GO-2026-4736"
	upstreamGovulncheckInstall = "golang.org/x/vuln/cmd/govulncheck@v1.3.0"
)

type semver struct {
	major int
	minor int
	patch int
}

func main() {
	slog.Info("enter govulncheck main")
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	slog.Info("start govulncheck wrapper", slog.Int("args", len(args)))
	moduleVersionValue, err := moduleVersion()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"govulncheck wrapper: resolve %s version: %v\n",
			gobgpModule,
			err,
		)
		return 1
	}

	output, exitCode, runErr := runUpstreamGovulncheck(args)
	if len(output) > 0 {
		if _, err := os.Stdout.Write(output); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "govulncheck wrapper: write output: %v\n", err)
			return 1
		}
	}

	if runErr == nil {
		return 0
	}

	if isKnownFalsePositive(output, moduleVersionValue) {
		if len(output) > 0 && !bytes.HasSuffix(output, []byte("\n")) {
			_, _ = fmt.Fprintln(os.Stdout)
		}
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintf(
			os.Stdout,
			"govulncheck: suppressing %s for %s %s\n",
			knownGobgpVulnID,
			gobgpModule,
			moduleVersionValue,
		)
		_, _ = fmt.Fprintln(
			os.Stdout,
			"govulncheck: upstream fix is present in v4.3.0+; current vuln metadata appears stale",
		)
		return 0
	}

	if exitCode != 0 {
		return exitCode
	}

	_, _ = fmt.Fprintf(os.Stderr, "govulncheck wrapper: %v\n", runErr)
	return 1
}

func moduleVersion() (string, error) {
	slog.Info("resolve gobgp module version")
	cmd := exec.CommandContext(
		context.Background(),
		"go",
		"list",
		"-m",
		"-f",
		"{{.Version}}",
		gobgpModule,
	)
	output, err := cmd.Output()
	if err != nil {
		slog.Error("resolve gobgp module version failed", slog.Any("err", err))
		return "", fmt.Errorf("go list gobgp module version: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func runUpstreamGovulncheck(args []string) ([]byte, int, error) {
	commandArgs := []string{"go", "run", upstreamGovulncheckInstall}
	commandArgs = append(commandArgs, args...)

	slog.Info("run upstream govulncheck", slog.Int("args", len(args)))
	cmd := exec.CommandContext(context.Background(), "go")
	cmd.Args = commandArgs
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		slog.Error(
			"run upstream govulncheck failed",
			slog.Any("err", err),
			slog.Int("exit_code", exitErr.ExitCode()),
		)
		return output, exitErr.ExitCode(), fmt.Errorf("run upstream govulncheck: %w", err)
	}

	slog.Error("run upstream govulncheck failed", slog.Any("err", err))
	return output, 0, fmt.Errorf("run upstream govulncheck: %w", err)
}

func isKnownFalsePositive(output []byte, moduleVersionValue string) bool {
	if !semverGTE(moduleVersionValue, knownGobgpFixedVersion) {
		return false
	}

	lines := strings.Split(string(output), "\n")
	vulnerabilityCount := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Vulnerability #") {
			vulnerabilityCount++
		}
	}
	if vulnerabilityCount != 1 {
		return false
	}

	if !bytes.Contains(output, []byte(knownGobgpVulnID)) {
		return false
	}

	return bytes.Contains(output, []byte("Module: "+gobgpModule))
}

func semverGTE(leftVersion string, rightVersion string) bool {
	left, err := parseSemver(leftVersion)
	if err != nil {
		return false
	}

	right, err := parseSemver(rightVersion)
	if err != nil {
		return false
	}

	if left.major != right.major {
		return left.major > right.major
	}
	if left.minor != right.minor {
		return left.minor > right.minor
	}
	return left.patch >= right.patch
}

func parseSemver(version string) (semver, error) {
	trimmedVersion := strings.TrimPrefix(version, "v")
	trimmedVersion = strings.SplitN(trimmedVersion, "-", 2)[0]
	trimmedVersion = strings.SplitN(trimmedVersion, "+", 2)[0]

	parts := strings.Split(trimmedVersion, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return semver{}, fmt.Errorf("invalid semver %q", version)
	}

	if len(parts) == 2 {
		parts = append(parts, "0")
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semver %q", version)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semver %q", version)
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semver %q", version)
	}

	return semver{
		major: major,
		minor: minor,
		patch: patch,
	}, nil
}
