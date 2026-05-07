package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

func TestBuildProbeExecRequestPrefersRepeatableArgs(t *testing.T) {
	stdinPath := writeTempProbeInput(t, []byte("payload"))
	req, err := buildProbeExecRequest(probeRPCArgs{
		cmd:          "/bin/cat",
		cmdArgs:      "legacy,ignored",
		cmdArgv:      []string{"first", "second,kept"},
		cmdSudo:      true,
		cmdStdinFile: stdinPath,
		cmdTimeout:   1500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.GetCommand() != "/bin/cat" {
		t.Fatalf("command=%q", req.GetCommand())
	}
	if got := req.GetArgs(); !slicesEqual(got, []string{"first", "second,kept"}) {
		t.Fatalf("args=%q", got)
	}
	if !req.GetSudo() {
		t.Fatal("expected sudo=true")
	}
	if req.GetTimeoutSeconds() != 2 {
		t.Fatalf("timeout_seconds=%d", req.GetTimeoutSeconds())
	}
	if !bytes.Equal(req.GetStdinBytes(), []byte("payload")) {
		t.Fatalf("stdin=%q", req.GetStdinBytes())
	}
}

func TestBuildProbeExecRequestKeepsLegacyCommaArgs(t *testing.T) {
	req, err := buildProbeExecRequest(probeRPCArgs{
		cmd:     "uname",
		cmdArgs: "-s,-r,-m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := req.GetArgs(); !slicesEqual(got, []string{"-s", "-r", "-m"}) {
		t.Fatalf("args=%q", got)
	}
}

func TestBuildProbeConfigctlRequestUsesActionTokens(t *testing.T) {
	req, err := buildProbeConfigctlRequest(
		[]string{"system", "event", "config_changed"},
		3*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if req.GetCommand() != opnsenseProbeConfigctlCommand {
		t.Fatalf("command=%q", req.GetCommand())
	}
	if got := req.GetArgs(); !slicesEqual(got, []string{"system", "event", "config_changed"}) {
		t.Fatalf("args=%q", got)
	}
	if req.GetTimeoutSeconds() != 3 {
		t.Fatalf("timeout_seconds=%d", req.GetTimeoutSeconds())
	}
}

func TestBuildProbeConfigctlRequestRequiresTokens(t *testing.T) {
	_, err := buildProbeConfigctlRequest(nil, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "op=configctl requires action tokens") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateProbeExecResponseTreatsConfigctlTextAsFailure(t *testing.T) {
	cases := []struct {
		name string
		resp *mwanv1.ExecResponse
	}{
		{
			name: "stdout",
			resp: &mwanv1.ExecResponse{
				Stdout:   []byte("Action not allowed or missing\n"),
				ExitCode: 0,
			},
		},
		{
			name: "stderr",
			resp: &mwanv1.ExecResponse{
				Stderr:   []byte("Action not allowed or missing\n"),
				ExitCode: 0,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProbeExecResponse("configctl", tt.resp)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), configctlActionNotAllowedText) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func writeTempProbeInput(t *testing.T, content []byte) string {
	t.Helper()
	path := t.TempDir() + "/stdin.txt"
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func slicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
