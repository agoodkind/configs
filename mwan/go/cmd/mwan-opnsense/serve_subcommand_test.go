package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestInspectPidfileMissing(t *testing.T) {
	pid, state, err := inspectPidfile(filepath.Join(t.TempDir(), "missing.pid"))
	if err != nil {
		t.Fatalf("inspectPidfile returned error: %v", err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0", pid)
	}
	if state != pidfileMissing {
		t.Fatalf("state = %v, want %v", state, pidfileMissing)
	}
}

func TestInspectPidfileInvalid(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "invalid.pid")
	if err := os.WriteFile(pidfile, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	pid, state, err := inspectPidfile(pidfile)
	if err == nil {
		t.Fatal("inspectPidfile returned nil error")
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0", pid)
	}
	if state != pidfileInvalid {
		t.Fatalf("state = %v, want %v", state, pidfileInvalid)
	}
}

func TestInspectPidfileRunning(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "running.pid")
	pid := os.Getpid()
	if err := os.WriteFile(pidfile, []byte("123\n"), 0o600); err != nil {
		t.Fatalf("write placeholder pidfile: %v", err)
	}
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	actualPid, state, err := inspectPidfile(pidfile)
	if err != nil {
		t.Fatalf("inspectPidfile returned error: %v", err)
	}
	if actualPid != pid {
		t.Fatalf("pid = %d, want %d", actualPid, pid)
	}
	if state != pidfileRunning {
		t.Fatalf("state = %v, want %v", state, pidfileRunning)
	}
}

func TestRunStatusReturnsOneForMissingPidfile(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "missing.pid")
	status := runStatus([]string{"-pidfile", pidfile, "-quiet"})
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
}
