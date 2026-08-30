package main

// These tests run the deploy play's verdict collector script end to end with
// a stubbed ssh on PATH, because the defect they lock down lives in the
// collector's probe ordering, not in the gate binary: a verdict recorded
// between the collector's failed read and its unit status probe must still
// be collected once the probe reports the transient unit inactive.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	collectorScriptRelPath = "../../../../ansible/playbooks/files/collect-deploy-verdict.sh"
	collectorSSHStubPath   = "testdata/collectverdict/ssh-stub.sh"
	collectorTraceID       = "trace-1"
	collectorOldBootID     = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	collectorTimeout       = 60 * time.Second
)

// collectorTestVerdict mirrors the gate's verdict JSON contract without
// depending on the linux-tagged gate sources, so the test builds on every
// platform the module builds on.
type collectorTestVerdict struct {
	TraceID    string `json:"trace_id"`
	OldBootID  string `json:"old_boot_id"`
	RebootRC   int    `json:"reboot_rc"`
	EgressRC   int    `json:"egress_rc"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

// runVerdictCollector executes the real collector script against the ssh
// stub. catFailures is the number of leading verdict reads that fail with
// ENOENT; verdictContent, when non-nil, is the verdict the stub serves after
// those failures.
func runVerdictCollector(
	t *testing.T,
	catFailures string,
	verdictContent []byte,
) (int, string, string) {
	t.Helper()
	return runVerdictCollectorWithTransport(t, catFailures, "0", verdictContent)
}

// runVerdictCollectorWithTransport is runVerdictCollector plus the stub's
// transport-failure threshold: reads past transportAfter fail with ssh's 255
// instead of reaching the host.
func runVerdictCollectorWithTransport(
	t *testing.T,
	catFailures string,
	transportAfter string,
	verdictContent []byte,
) (int, string, string) {
	t.Helper()

	scriptPath, err := filepath.Abs(collectorScriptRelPath)
	if err != nil {
		t.Fatalf("resolve collector script path: %v", err)
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("collector script missing: %v", err)
	}
	stubSourcePath, err := filepath.Abs(collectorSSHStubPath)
	if err != nil {
		t.Fatalf("resolve ssh stub path: %v", err)
	}

	stateDir := t.TempDir()
	stubBinDir := filepath.Join(stateDir, "bin")
	if err := os.MkdirAll(stubBinDir, 0o755); err != nil {
		t.Fatalf("create stub bin dir: %v", err)
	}
	stubContent, err := os.ReadFile(stubSourcePath)
	if err != nil {
		t.Fatalf("read ssh stub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stubBinDir, "ssh"), stubContent, 0o755); err != nil {
		t.Fatalf("install ssh stub: %v", err)
	}
	if verdictContent != nil {
		verdictFile := filepath.Join(stateDir, "verdict.json")
		if err := os.WriteFile(verdictFile, verdictContent, 0o644); err != nil {
			t.Fatalf("stage verdict: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), collectorTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, scriptPath,
		"/run/mwan-deploy-gate/"+collectorTraceID+".json",
		"mwan-deploy-gate-"+collectorTraceID,
		collectorTraceID,
		collectorOldBootID,
		"30",
		"192.0.2.10",
	)
	cmd.Env = append(os.Environ(),
		"PATH="+stubBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_STATE_DIR="+stateDir,
		"STUB_CAT_FAILURES="+catFailures,
		"STUB_CAT_TRANSPORT_AFTER="+transportAfter,
	)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("run collector: %v\nstderr: %s", runErr, stderr.String())
		}
		exitCode = exitErr.ExitCode()
	}
	return exitCode, stdout.String(), stderr.String()
}

func marshalCollectorVerdict(t *testing.T, verdict collectorTestVerdict) []byte {
	t.Helper()
	payload, err := json.Marshal(verdict)
	if err != nil {
		t.Fatalf("marshal verdict: %v", err)
	}
	return payload
}

// The reproduced race: the read fails while the gate is still running, then
// the gate records the verdict and exits, and systemd-run --collect
// garbage-collects the unit so the status probe reports inactive. The
// collector must re-read and deliver the verdict instead of declaring gate
// death.
func TestCollectorCollectsVerdictRecordedBetweenReadAndStatusProbe(t *testing.T) {
	verdict := collectorTestVerdict{
		TraceID:    collectorTraceID,
		OldBootID:  collectorOldBootID,
		RebootRC:   0,
		EgressRC:   0,
		StartedAt:  "2026-08-29T18:35:58Z",
		FinishedAt: "2026-08-29T18:37:12Z",
	}

	exitCode, stdout, stderr := runVerdictCollector(
		t, "1", marshalCollectorVerdict(t, verdict))

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", exitCode, stderr)
	}
	var collected collectorTestVerdict
	if err := json.Unmarshal([]byte(stdout), &collected); err != nil {
		t.Fatalf("stdout is not the verdict JSON: %v\nstdout: %s", err, stdout)
	}
	if collected != verdict {
		t.Fatalf("collected verdict = %+v, want %+v", collected, verdict)
	}
	if !strings.Contains(stderr, "Collected deploy verdict") {
		t.Fatalf("stderr does not confirm collection: %s", stderr)
	}
}

func TestCollectorConfirmsGateDeathWhenNoVerdictEverAppears(t *testing.T) {
	exitCode, stdout, stderr := runVerdictCollector(t, "100", nil)

	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2\nstderr: %s", exitCode, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "gate death confirmed") {
		t.Fatalf("stderr does not confirm gate death: %s", stderr)
	}
}

// A verdict keyed to another run proves nothing about this one, so an
// inactive unit with only a stale verdict still means this run's gate died
// without recording.
func TestCollectorRejectsStaleVerdictAfterInactiveUnit(t *testing.T) {
	staleVerdict := collectorTestVerdict{
		TraceID:    "trace-0-previous-run",
		OldBootID:  collectorOldBootID,
		RebootRC:   0,
		EgressRC:   0,
		StartedAt:  "2026-08-28T00:00:00Z",
		FinishedAt: "2026-08-28T00:02:00Z",
	}

	exitCode, stdout, stderr := runVerdictCollector(
		t, "1", marshalCollectorVerdict(t, staleVerdict))

	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2\nstderr: %s", exitCode, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "Stale-verdict rejection") {
		t.Fatalf("stderr does not report the stale rejection: %s", stderr)
	}
	if !strings.Contains(stderr, "gate death confirmed") {
		t.Fatalf("stderr does not confirm gate death: %s", stderr)
	}
}

// A re-read that loses connectivity observes nothing about the verdict, so it
// must not confirm gate death: that would repeat the defect this collector fix
// exists to correct, failing a deploy whose gate may well have recorded a
// passing verdict. The run keeps polling and ends at the timeout instead.
func TestCollectorDoesNotConfirmDeathWhenReReadLosesConnectivity(t *testing.T) {
	verdict := collectorTestVerdict{
		TraceID:    collectorTraceID,
		OldBootID:  collectorOldBootID,
		RebootRC:   0,
		EgressRC:   0,
		StartedAt:  "2026-08-29T00:00:00Z",
		FinishedAt: "2026-08-29T00:02:00Z",
	}

	// The first read fails with ENOENT, the status probe reports the unit
	// inactive, and every read from the second on fails with ssh's 255.
	exitCode, stdout, stderr := runVerdictCollectorWithTransport(
		t, "1", "1", marshalCollectorVerdict(t, verdict))

	if exitCode == 2 {
		t.Fatalf("collector confirmed gate death from a transport failure\nstderr: %s", stderr)
	}
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1 (timeout)\nstderr: %s", exitCode, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "not confirming gate death") {
		t.Fatalf("stderr does not report the withheld verdict: %s", stderr)
	}
	if strings.Contains(stderr, "gate death confirmed") {
		t.Fatalf("stderr confirms gate death despite losing the connection: %s", stderr)
	}
}
