package opnsensesvc

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunExec_Echo(t *testing.T) {
	res, err := runExec(context.Background(), ExecArgs{
		Command: "/bin/sh",
		Args:    []string{"-c", "echo hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(string(res.Stdout), "hello") {
		t.Fatalf("stdout=%q", res.Stdout)
	}
	if res.TimedOut {
		t.Fatal("did not expect TimedOut")
	}
	if res.StdoutTruncated {
		t.Fatal("did not expect StdoutTruncated")
	}
}

func TestRunExec_NonzeroExit(t *testing.T) {
	res, err := runExec(context.Background(), ExecArgs{
		Command: "/bin/sh",
		Args:    []string{"-c", "exit 7"},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.ExitCode != 7 {
		t.Fatalf("expected exit=7, got %d", res.ExitCode)
	}
}

func TestRunExec_Stderr(t *testing.T) {
	res, err := runExec(context.Background(), ExecArgs{
		Command: "/bin/sh",
		Args:    []string{"-c", "echo oops 1>&2; exit 0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Stderr), "oops") {
		t.Fatalf("stderr=%q", res.Stderr)
	}
}

func TestRunExec_Timeout(t *testing.T) {
	start := time.Now()
	res, err := runExec(context.Background(), ExecArgs{
		Command:        "/bin/sh",
		Args:           []string{"-c", "sleep 5"},
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	dur := time.Since(start)
	if !res.TimedOut {
		t.Fatalf("expected TimedOut=true; result=%+v", res)
	}
	if dur > 3*time.Second {
		t.Fatalf("timeout enforcement slow: %v", dur)
	}
}

func TestRunExec_Stdin(t *testing.T) {
	res, err := runExec(context.Background(), ExecArgs{
		Command:    "/bin/cat",
		StdinBytes: []byte("piped"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res.Stdout, []byte("piped")) {
		t.Fatalf("stdout=%q", res.Stdout)
	}
}

func TestRunExec_OutputCap(t *testing.T) {
	// emit ~12 MB; cap is 10 MB
	res, err := runExec(context.Background(), ExecArgs{
		Command: "/bin/sh",
		Args:    []string{"-c", "yes hello | head -c 12000000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.StdoutTruncated {
		t.Fatal("expected StdoutTruncated=true")
	}
	if len(res.Stdout) > maxOutputBytes {
		t.Fatalf("stdout exceeded cap: %d > %d", len(res.Stdout), maxOutputBytes)
	}
}

func TestValidateExecArgs(t *testing.T) {
	cases := []struct {
		name string
		args ExecArgs
		ok   bool
	}{
		{"empty cmd", ExecArgs{Command: ""}, false},
		{"null in cmd", ExecArgs{Command: "/bin/sh\x00x"}, false},
		{"null in arg", ExecArgs{Command: "/bin/sh", Args: []string{"\x00"}}, false},
		{"valid", ExecArgs{Command: "/bin/sh", Args: []string{"-c", "true"}}, true},
		{"long cmd", ExecArgs{Command: strings.Repeat("a", 5000)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateExecArgs(c.args)
			if (err == nil) != c.ok {
				t.Fatalf("validateExecArgs ok=%v err=%v", c.ok, err)
			}
		})
	}
}

func TestRunExec_BinaryNotFound(t *testing.T) {
	_, err := runExec(context.Background(), ExecArgs{Command: "/this/does/not/exist"})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestRunExec_TimeoutClamp(t *testing.T) {
	// Caller asks for 99999s; we cap at maxExecTimeout (5min). Just
	// ensure the call returns quickly when the command is fast.
	res, err := runExec(context.Background(), ExecArgs{
		Command:        "/bin/sh",
		Args:           []string{"-c", "exit 0"},
		TimeoutSeconds: 99999,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
}
