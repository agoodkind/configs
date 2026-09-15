package main

// These tests run the OPNsense deploy's instance script against real processes.
// Each fake instance is a sleep started with the argv[0] that a daemon(8)
// supervisor or an mwan-opnsense daemon shows in ps, under paths inside the
// test's own temp directory, so the script's matching, signalling, and
// escalation run for real and never reach a process outside the test.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	instancesScriptRelPath = "../../../../ansible/playbooks/files/mwan-opnsense-instances.sh"
	instancesRunTimeout    = 90 * time.Second
	instancesWaitSeconds   = "2"
	fakeInstanceSleep      = "300"
	processPollInterval    = 50 * time.Millisecond
	processPollTimeout     = 10 * time.Second
)

// instanceLayout is one test's fake guest paths. Nothing exists at runShim or
// daemonBinary; those paths only appear in the fake processes' command lines.
type instanceLayout struct {
	runShim      string
	daemonBinary string
	pidfile      string
	rcScript     string
	rcLog        string
	dir          string
}

func newInstanceLayout(t *testing.T) instanceLayout {
	t.Helper()
	dir := t.TempDir()
	layout := instanceLayout{
		runShim:      filepath.Join(dir, "libexec", "mwan-opnsense-run"),
		daemonBinary: filepath.Join(dir, "sbin", "mwan-opnsense"),
		pidfile:      filepath.Join(dir, "mwan_opnsense.pid"),
		rcScript:     filepath.Join(dir, "rc.d-mwan_opnsense"),
		rcLog:        filepath.Join(dir, "rc.log"),
		dir:          dir,
	}
	// The rc.d stand-in records its verb and exits 1, the way the real stop does
	// on an invalid pidfile, so the sweep cannot lean on the rc.d stop.
	rcText := "#!/bin/sh\necho \"$1\" >> \"" + layout.rcLog + "\"\nexit 1\n"
	if err := os.WriteFile(layout.rcScript, []byte(rcText), 0o700); err != nil {
		t.Fatalf("write rc.d stand-in: %v", err)
	}
	return layout
}

// startFake starts one process whose ps command line begins with argv0 and
// waits until ps shows it. With ignoreTerm the process ignores SIGTERM, like a
// daemon stuck in a serial write, so only SIGKILL ends it.
func startFake(t *testing.T, argv0 string, ignoreTerm bool) int {
	t.Helper()
	script := `exec -a "$0" sleep ` + fakeInstanceSleep
	if ignoreTerm {
		script = `trap "" TERM; ` + script
	}
	command := exec.CommandContext(t.Context(), "bash", "-c", script, argv0)
	if err := command.Start(); err != nil {
		t.Fatalf("start fake %q: %v", argv0, err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-reaped
	})
	pid := command.Process.Pid
	waitForCommandPrefix(t, pid, argv0)
	return pid
}

// startFakeSupervisor starts a fake supervisor and a fake daemon whose parent it
// is, and returns both pids. Unlike daemon(8), the fake supervisor does not
// forward SIGTERM, so a TERM leaves its daemon orphaned, which is the shape the
// deploy must still sweep.
func startFakeSupervisor(t *testing.T, supervisorArgv0, daemonArgv0 string) (int, int) {
	t.Helper()
	script := `exec -a "$1" sleep ` + fakeInstanceSleep + ` >/dev/null 2>&1 & echo "$!"; exec -a "$0" sleep ` + fakeInstanceSleep
	command := exec.CommandContext(t.Context(), "bash", "-c", script, supervisorArgv0, daemonArgv0)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("fake supervisor stdout: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start fake supervisor: %v", err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read fake daemon pid: %v", err)
	}
	daemonPid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse fake daemon pid %q: %v", line, err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(daemonPid, syscall.SIGKILL)
		_ = command.Process.Kill()
		<-reaped
	})
	supervisorPid := command.Process.Pid
	waitForCommandPrefix(t, supervisorPid, supervisorArgv0)
	waitForCommandPrefix(t, daemonPid, daemonArgv0)
	return supervisorPid, daemonPid
}

// exitedPid returns the pid of a process that has already exited, which no
// live process owns.
func exitedPid(t *testing.T) int {
	t.Helper()
	command := exec.CommandContext(t.Context(), "true")
	if err := command.Run(); err != nil {
		t.Fatalf("run true: %v", err)
	}
	return command.ProcessState.Pid()
}

// processCommand returns the ps command line of pid, and false when pid is not
// a live process. A zombie counts as gone.
func processCommand(t *testing.T, pid int) (string, bool) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "ps", "-o", "stat=", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", false
		}
		t.Fatalf("ps -p %d: %v", pid, err)
	}
	fields := strings.SplitN(strings.TrimSpace(string(output)), " ", 2)
	if len(fields) < 2 || strings.HasPrefix(fields[0], "Z") {
		return "", false
	}
	return strings.TrimSpace(fields[1]), true
}

func waitForCommandPrefix(t *testing.T, pid int, prefix string) {
	t.Helper()
	deadline := time.Now().Add(processPollTimeout)
	for {
		command, running := processCommand(t, pid)
		if running && strings.HasPrefix(command, prefix) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d never showed command %q (last %q, running %t)", pid, prefix, command, running)
		}
		time.Sleep(processPollInterval)
	}
}

func runInstancesScript(t *testing.T, extraPath string, args ...string) (int, string, string) {
	t.Helper()
	scriptPath, err := filepath.Abs(instancesScriptRelPath)
	if err != nil {
		t.Fatalf("resolve instance script path: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), instancesRunTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", append([]string{scriptPath}, args...)...)
	if extraPath != "" {
		command.Env = append(os.Environ(), "PATH="+extraPath+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	var stdout strings.Builder
	var stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("run instance script: %v\nstderr: %s", runErr, stderr.String())
		}
		exitCode = exitErr.ExitCode()
	}
	return exitCode, stdout.String(), stderr.String()
}

func writePidfile(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
}

// The production failure: the instance the pidfile tracks, a supervisor from an
// older rc.d script that the pidfile no longer names, and a daemon that ignores
// SIGTERM all run at once. stop must end every one of them, escalate to KILL for
// the stubborn daemon, and leave a process that merely names the daemon path
// alone.
func TestInstancesStopEndsEveryInstance(t *testing.T) {
	t.Parallel()
	layout := newInstanceLayout(t)

	trackedSupervisor, trackedDaemon := startFakeSupervisor(t,
		"daemon: "+layout.runShim+"[1] (daemon)", layout.daemonBinary)
	oldSupervisor, oldDaemon := startFakeSupervisor(t,
		"daemon: "+layout.daemonBinary+"[1] (daemon)", "/bin/sh "+layout.runShim)
	stubbornDaemon := startFake(t, layout.daemonBinary+".current", true)
	bystander := startFake(t, "less "+layout.daemonBinary, false)
	writePidfile(t, layout.pidfile, trackedSupervisor)

	exitCode, _, stderr := runInstancesScript(t, "", "stop",
		layout.rcScript, layout.runShim, layout.daemonBinary, instancesWaitSeconds)
	if exitCode != 0 {
		t.Fatalf("stop exit code = %d, want 0\nstderr:\n%s", exitCode, stderr)
	}

	rcLog, err := os.ReadFile(layout.rcLog)
	if err != nil || strings.TrimSpace(string(rcLog)) != "stop" {
		t.Fatalf("rc.d stand-in log = %q (err %v), want one stop", rcLog, err)
	}
	instances := map[string]int{
		"tracked supervisor": trackedSupervisor,
		"tracked daemon":     trackedDaemon,
		"old supervisor":     oldSupervisor,
		"old daemon":         oldDaemon,
		"stubborn daemon":    stubbornDaemon,
	}
	for name, pid := range instances {
		if command, running := processCommand(t, pid); running {
			t.Errorf("%s pid %d still runs %q after stop", name, pid, command)
		}
	}
	if !strings.Contains(stderr, "sending KILL to daemon pids "+strconv.Itoa(stubbornDaemon)) {
		t.Errorf("stop did not escalate to KILL for the stubborn daemon\nstderr:\n%s", stderr)
	}
	if _, running := processCommand(t, bystander); !running {
		t.Errorf("stop ended pid %d, which only names the daemon path", bystander)
	}
}

// A daemon wedged in the kernel survives SIGKILL until its write returns. The
// deploy must then fail rather than start a second reader, so stop must exit 1
// and name the survivor. A ps stand-in reports a survivor that no signal can
// reach, because a process that outlives SIGKILL cannot be made in a test.
func TestInstancesStopFailsWhileAnInstanceSurvives(t *testing.T) {
	t.Parallel()
	layout := newInstanceLayout(t)
	survivor := exitedPid(t)

	binDir := filepath.Join(layout.dir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create ps stand-in dir: %v", err)
	}
	psLine := fmt.Sprintf("%d 1 daemon: %s[%d] (daemon)", survivor, layout.runShim, survivor)
	psText := "#!/bin/sh\necho \"" + psLine + "\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "ps"), []byte(psText), 0o700); err != nil {
		t.Fatalf("write ps stand-in: %v", err)
	}

	exitCode, _, stderr := runInstancesScript(t, binDir, "stop",
		layout.rcScript, layout.runShim, layout.daemonBinary, "0")
	if exitCode != 1 {
		t.Fatalf("stop exit code = %d, want 1\nstderr:\n%s", exitCode, stderr)
	}
	wantSurvivor := fmt.Sprintf("%d 1 supervisor", survivor)
	if !strings.Contains(stderr, "instances still running after KILL") || !strings.Contains(stderr, wantSurvivor) {
		t.Fatalf("stop did not name the survivor %q\nstderr:\n%s", wantSurvivor, stderr)
	}
}

func TestInstancesCheckOne(t *testing.T) {
	t.Parallel()

	t.Run("one instance the pidfile names", func(t *testing.T) {
		t.Parallel()
		layout := newInstanceLayout(t)
		supervisor, daemon := startFakeSupervisor(t, "daemon: "+layout.runShim+"[1] (daemon)", layout.daemonBinary)
		writePidfile(t, layout.pidfile, supervisor)

		exitCode, stdout, stderr := runInstancesScript(t, "", "check-one",
			layout.runShim, layout.daemonBinary, layout.pidfile)
		if exitCode != 0 {
			t.Fatalf("check-one exit code = %d, want 0\nstderr:\n%s", exitCode, stderr)
		}
		want := fmt.Sprintf("supervisor=%d daemon=%d\n", supervisor, daemon)
		if stdout != want {
			t.Fatalf("check-one stdout = %q, want %q", stdout, want)
		}
	})

	t.Run("a second instance the pidfile does not name", func(t *testing.T) {
		t.Parallel()
		layout := newInstanceLayout(t)
		tracked, _ := startFakeSupervisor(t, "daemon: "+layout.runShim+"[1] (daemon)", layout.daemonBinary)
		startFakeSupervisor(t, "daemon: "+layout.daemonBinary+"[1] (daemon)", layout.daemonBinary)
		writePidfile(t, layout.pidfile, tracked)

		exitCode, _, stderr := runInstancesScript(t, "", "check-one",
			layout.runShim, layout.daemonBinary, layout.pidfile)
		if exitCode != 1 {
			t.Fatalf("check-one exit code = %d, want 1\nstderr:\n%s", exitCode, stderr)
		}
		if !strings.Contains(stderr, "found 2 supervisor(s) and 2 daemon(s)") {
			t.Fatalf("check-one did not report both instances\nstderr:\n%s", stderr)
		}
	})

	t.Run("one instance the pidfile does not name", func(t *testing.T) {
		t.Parallel()
		layout := newInstanceLayout(t)
		startFakeSupervisor(t, "daemon: "+layout.runShim+"[1] (daemon)", layout.daemonBinary)
		writePidfile(t, layout.pidfile, exitedPid(t))

		exitCode, _, stderr := runInstancesScript(t, "", "check-one",
			layout.runShim, layout.daemonBinary, layout.pidfile)
		if exitCode != 1 {
			t.Fatalf("check-one exit code = %d, want 1\nstderr:\n%s", exitCode, stderr)
		}
	})

	t.Run("no instance", func(t *testing.T) {
		t.Parallel()
		layout := newInstanceLayout(t)

		exitCode, _, stderr := runInstancesScript(t, "", "check-one",
			layout.runShim, layout.daemonBinary, layout.pidfile)
		if exitCode != 1 {
			t.Fatalf("check-one exit code = %d, want 1\nstderr:\n%s", exitCode, stderr)
		}
		if !strings.Contains(stderr, "found 0 supervisor(s) and 0 daemon(s)") {
			t.Fatalf("check-one did not report zero instances\nstderr:\n%s", stderr)
		}
	})
}
