// Package packageupdater tests the weekly package updater script that
// hypervisors and guests run from a systemd timer. The tests run the real
// script under bash with fake apt-get and dpkg first on PATH.
package packageupdater

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	scriptRelPath   = "../../../common/scripts/package-updater.sh"
	recordEnv       = "PACKAGE_UPDATER_RECORD"
	updateGateEnv   = "FAKE_APT_GET_UPDATE_GATE"
	dpkgExitCodeEnv = "FAKE_DPKG_EXIT_CODE"

	configureCall   = "dpkg --force-confold --configure -a"
	updateCall      = "apt-get update"
	updateDone      = "apt-get update finished"
	fullUpgradeCall = "apt-get -o Dpkg::Options::=--force-confold full-upgrade -y"
	autoremoveCall  = "apt-get autoremove -y"
	autocleanCall   = "apt-get autoclean"

	stoppedExitCode = 143
	waitTimeout     = 10 * time.Second
	pollInterval    = 20 * time.Millisecond
)

type harness struct {
	script string
	record string
	gate   string
	env    []string
}

func newHarness(t *testing.T, dpkgExitCode int) harness {
	t.Helper()

	script, err := filepath.Abs(scriptRelPath)
	if err != nil {
		t.Fatalf("resolve %s: %v", scriptRelPath, err)
	}
	fakeBin := t.TempDir()
	installFake(t, "fake-apt-get.sh", filepath.Join(fakeBin, "apt-get"))
	installFake(t, "fake-dpkg.sh", filepath.Join(fakeBin, "dpkg"))

	work := t.TempDir()
	record := filepath.Join(work, "record")
	gate := filepath.Join(work, "update-gate")
	env := append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		recordEnv+"="+record,
		updateGateEnv+"="+gate,
		dpkgExitCodeEnv+"="+strconv.Itoa(dpkgExitCode),
	)
	return harness{script: script, record: record, gate: gate, env: env}
}

func installFake(t *testing.T, name, target string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := os.WriteFile(target, body, 0o700); err != nil {
		t.Fatalf("write %s: %v", target, err)
	}
}

func (h harness) command(output *bytes.Buffer) *exec.Cmd {
	command := exec.Command("bash", h.script)
	command.Env = h.env
	command.Stdout = output
	command.Stderr = output
	return command
}

func (h harness) openGate(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(h.gate, nil, 0o600); err != nil {
		t.Fatalf("open update gate: %v", err)
	}
}

func (h harness) calls(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(h.record)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read call record: %v", err)
	}
	return strings.Split(strings.TrimRight(string(body), "\n"), "\n")
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run script: %v", err)
	}
	return exitErr.ExitCode()
}

// TestConfiguresPendingPackagesBeforeUpgrading pins the healing step: a run
// that an earlier stop or timeout interrupted mid-configure leaves packages
// half configured, and apt-get refuses to upgrade until dpkg configures them.
func TestConfiguresPendingPackagesBeforeUpgrading(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 0)
	h.openGate(t)

	var output bytes.Buffer
	if code := exitCode(t, h.command(&output).Run()); code != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", code, output.String())
	}

	want := []string{configureCall, updateCall, updateDone, fullUpgradeCall, autoremoveCall, autocleanCall}
	if got := h.calls(t); !slices.Equal(got, want) {
		t.Fatalf("calls = %q, want %q\n%s", got, want, output.String())
	}
	if !strings.Contains(output.String(), "dpkg --configure -a exit_code=0") {
		t.Fatalf("output does not log the configure exit code:\n%s", output.String())
	}
}

// TestStopsWhenConfiguringPendingPackagesFails pins that a configure failure
// is logged with its exit code and ends the run before apt-get touches the
// package set, and that the unit sees the same exit code.
func TestStopsWhenConfiguringPendingPackagesFails(t *testing.T) {
	t.Parallel()
	const dpkgFailure = 2
	h := newHarness(t, dpkgFailure)
	h.openGate(t)

	var output bytes.Buffer
	if code := exitCode(t, h.command(&output).Run()); code != dpkgFailure {
		t.Fatalf("exit code = %d, want %d\n%s", code, dpkgFailure, output.String())
	}

	want := []string{configureCall}
	if got := h.calls(t); !slices.Equal(got, want) {
		t.Fatalf("calls = %q, want %q\n%s", got, want, output.String())
	}
	if !strings.Contains(output.String(), "failed exit_code="+strconv.Itoa(dpkgFailure)) {
		t.Fatalf("output does not log the configure failure:\n%s", output.String())
	}
}

// TestFinishesTheRunningStepWhenStopped signals only the script, as the unit's
// KillMode=process does on stop, while apt-get is running. The running step
// must complete and the script must exit before starting the next one, so a
// stop never cuts dpkg off mid-configure.
func TestFinishesTheRunningStepWhenStopped(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 0)

	var output bytes.Buffer
	command := h.command(&output)
	if err := command.Start(); err != nil {
		t.Fatalf("start script: %v", err)
	}
	deadline := time.Now().Add(waitTimeout)
	for !slices.Contains(h.calls(t), updateCall) {
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatalf("apt-get update never started\n%s", output.String())
		}
		time.Sleep(pollInterval)
	}

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal script: %v", err)
	}
	h.openGate(t)
	code := exitCode(t, command.Wait())

	want := []string{configureCall, updateCall, updateDone}
	if got := h.calls(t); !slices.Equal(got, want) {
		t.Fatalf("calls = %q, want %q\n%s", got, want, output.String())
	}
	if code != stoppedExitCode {
		t.Fatalf("exit code = %d, want %d\n%s", code, stoppedExitCode, output.String())
	}
	if !strings.Contains(output.String(), "stopped before: "+fullUpgradeCall) {
		t.Fatalf("output does not log where the run stopped:\n%s", output.String())
	}
}
