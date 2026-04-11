package cutover

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SSHResult holds the output of a remote command.
type SSHResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// sshExec runs a command on a remote host via SSH with an explicit timeout.
// No shell interpretation on the local side. The command string is passed as a
// single argument to bash -c on the remote side for predictable behavior.
// sshTarget returns the SSH target. If host already contains @, use as-is.
// Otherwise prepend root@.
func sshTarget(host string) string {
	if strings.Contains(host, "@") {
		return host
	}
	return "root@" + host
}

func sshExec(ctx context.Context, host string, command string, timeoutSec int) (SSHResult, error) {
	timeout := time.Duration(timeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeoutSec),
		sshTarget(host),
		command,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := SSHResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil // non-zero exit is not a Go error; caller decides
		}
		if ctx.Err() != nil {
			return result, fmt.Errorf("ssh to %s timed out after %s: %w", host, timeout, ctx.Err())
		}
		return result, fmt.Errorf("ssh to %s failed: %w", host, err)
	}

	return result, nil
}

// sshMustExec runs a command and returns an error if it exits non-zero.
func sshMustExec(ctx context.Context, host string, command string, timeoutSec int) (string, error) {
	r, err := sshExec(ctx, host, command, timeoutSec)
	if err != nil {
		return "", err
	}
	if r.ExitCode != 0 {
		return "", fmt.Errorf("command on %s exited %d: stdout=%q stderr=%q",
			host, r.ExitCode, r.Stdout, r.Stderr)
	}
	return r.Stdout, nil
}

// localExec runs a command locally with a timeout.
func localExec(ctx context.Context, name string, args []string, timeoutSec int) (string, error) {
	timeout := time.Duration(timeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s timed out after %s", name, timeout)
		}
		return "", fmt.Errorf("%s failed: %w (stderr: %s)", name, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}
