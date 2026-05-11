package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"goodkind.io/mwan/internal/opnsensesvc"
)

const (
	defaultPidfile        = "/var/run/mwan_opnsense.pid"
	rcEnabledCheckTimeout = 5 * time.Second
)

type pidfileState int

const (
	pidfileMissing pidfileState = iota
	pidfileInvalid
	pidfileStale
	pidfileRunning
)

// runOPNsenseDaemonServe starts the MWN1 dispatcher daemon with the virtio-serial-pci listener.
// There is exactly one listener and exactly one peer. Auth is unix
// socket permissions on the host side (root-only), so the daemon does
// not authenticate at the application layer.
func runOPNsenseDaemonServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	serialPath := fs.String("serial", "/dev/ttyV0.1", "virtio-serial device path (short-RPC port)")
	longSerial := fs.String("serial-long", "",
		"optional second virtio-serial device for long-running RPCs (Exec/Deploy/Revert); empty disables channel split")
	configPath := fs.String("config-xml", opnsensesvc.ConfigPath, "OPNsense config.xml path")
	backupDir := fs.String("backup-dir", opnsensesvc.BackupDir, "directory for snapshot files")
	daemonize := fs.Bool("daemonize", false, "detach into the background")
	pidfile := fs.String("pidfile", "", "pidfile written by -daemonize")
	logfile := fs.String("logfile", "", "logfile used by -daemonize")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	if *serialPath == "" {
		fmt.Fprintln(os.Stderr, "serve: -serial path required")
		return 2
	}

	if *daemonize {
		if err := daemonizeServe(*serialPath, *longSerial, *configPath, *backupDir, *pidfile, *logfile); err != nil {
			fmt.Fprintf(os.Stderr, "serve: daemonize: %v\n", err)
			return 1
		}
		return 0
	}

	srv := opnsensesvc.NewServer(slog.Default(), *configPath, *backupDir)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	// We do not `defer cancel()` because we explicitly call it before
	// returning from this subcommand.

	opts := opnsensesvc.ServeOpts{
		SerialPath:     *serialPath,
		OpenSerial:     opnsensesvc.OpenVirtioSerial,
		Server:         srv,
		Log:            slog.Default(),
		OnSerialOpen:   nil,
		Watchdog:       opnsensesvc.DefaultWatchdogConfig(),
		LongSerialPath: *longSerial,
	}

	slog.Info("mwan-opnsense: serving", "serial_path", *serialPath)

	err := opnsensesvc.Serve(ctx, opts)
	cancel()
	if err != nil {
		slog.Error("serve: terminated", "err", err)
		return 1
	}
	slog.Info("mwan-opnsense: stopped")
	return 0
}

func daemonizeServe(serialPath, longSerialPath, configPath, backupDir, pidfile, logfile string) error {
	executable, err := os.Executable()
	if err != nil {
		wrappedErr := fmt.Errorf("resolve executable: %w", err)
		slog.Error("serve: daemonize failed", "err", wrappedErr)
		return wrappedErr
	}

	childArgs := []string{
		"serve",
		"-serial", serialPath,
		"-config-xml", configPath,
		"-backup-dir", backupDir,
	}
	if longSerialPath != "" {
		childArgs = append(childArgs, "-serial-long", longSerialPath)
	}
	if !invokedAsOPNsenseDaemon(executable) {
		childArgs = append([]string{"opnsense"}, childArgs...)
	}
	command := exec.CommandContext(context.Background(), executable, childArgs...)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Env = os.Environ()
	command.Env = append(command.Env, "MWAN_OPNSENSE_DAEMON_CHILD=1")

	stdin, err := os.Open(os.DevNull)
	if err != nil {
		wrappedErr := fmt.Errorf("open stdin: %w", err)
		slog.Error("serve: daemonize failed", "err", wrappedErr)
		return wrappedErr
	}
	defer func() { _ = stdin.Close() }()
	command.Stdin = stdin

	output, err := daemonizeOutput(logfile)
	if err != nil {
		return err
	}
	defer func() { _ = output.Close() }()
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		wrappedErr := fmt.Errorf("start child: %w", err)
		slog.Error("serve: daemonize failed", "err", wrappedErr)
		return wrappedErr
	}

	if pidfile != "" {
		pid := strconv.Itoa(command.Process.Pid) + "\n"
		if err := os.WriteFile(pidfile, []byte(pid), 0o600); err != nil {
			_ = command.Process.Kill()
			_ = command.Process.Release()
			wrappedErr := fmt.Errorf("write pidfile: %w", err)
			slog.Error("serve: daemonize failed", "err", wrappedErr)
			return wrappedErr
		}
	}

	if err := command.Process.Release(); err != nil {
		wrappedErr := fmt.Errorf("release child: %w", err)
		slog.Error("serve: daemonize failed", "err", wrappedErr)
		return wrappedErr
	}
	return nil
}

func daemonizeOutput(logfile string) (*os.File, error) {
	if logfile == "" {
		file, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			wrappedErr := fmt.Errorf("open output: %w", err)
			slog.Error("serve: daemonize failed", "err", wrappedErr)
			return nil, wrappedErr
		}
		return file, nil
	}

	file, err := os.OpenFile(logfile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		wrappedErr := fmt.Errorf("open logfile: %w", err)
		slog.Error("serve: daemonize failed", "err", wrappedErr)
		return nil, wrappedErr
	}
	return file, nil
}

func runOPNsenseDaemonStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	pidfile := fs.String("pidfile", defaultPidfile, "pidfile to inspect")
	quiet := fs.Bool("quiet", false, "suppress status output")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *pidfile == "" {
		fmt.Fprintln(os.Stderr, "status: -pidfile path required")
		return 2
	}

	pid, state, err := inspectPidfile(*pidfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan-opnsense status: %v\n", err)
		return 2
	}

	switch state {
	case pidfileRunning:
		if !*quiet {
			fmt.Fprintf(os.Stdout, "mwan-opnsense is running as pid %d\n", pid)
		}
		return 0
	case pidfileStale:
		if !*quiet {
			fmt.Fprintf(os.Stderr, "mwan-opnsense is not running; stale pid %d in %s\n", pid, *pidfile)
		}
		return 1
	case pidfileInvalid:
		if !*quiet {
			fmt.Fprintf(os.Stderr, "mwan-opnsense is not running; invalid pidfile %s\n", *pidfile)
		}
		return 1
	case pidfileMissing:
		if !*quiet {
			fmt.Fprintf(os.Stderr, "mwan-opnsense is not running; pidfile %s missing\n", *pidfile)
		}
		return 1
	default:
		fmt.Fprintln(os.Stderr, "mwan-opnsense status: unknown pidfile state")
		return 2
	}
}

func inspectPidfile(pidfile string) (int, pidfileState, error) {
	content, readErr := os.ReadFile(pidfile)
	if errors.Is(readErr, os.ErrNotExist) {
		return 0, pidfileMissing, nil
	}
	if readErr != nil {
		slog.Error("status: read pidfile failed", "err", readErr, "pidfile", pidfile)
		return 0, pidfileInvalid, fmt.Errorf("read pidfile %s: %w", pidfile, readErr)
	}

	pidText := strings.TrimSpace(string(content))
	pid, convertErr := strconv.Atoi(pidText)
	if convertErr != nil {
		return 0, pidfileInvalid, fmt.Errorf("parse pidfile %s: %w", pidfile, convertErr)
	}
	if pid <= 0 {
		return 0, pidfileInvalid, nil
	}

	running, err := processRunning(pid)
	if err != nil {
		slog.Error("status: inspect pid failed", "err", err, "pid", pid)
		return pid, pidfileInvalid, fmt.Errorf("inspect pid %d: %w", pid, err)
	}
	if !running {
		return pid, pidfileStale, nil
	}
	return pid, pidfileRunning, nil
}

func processRunning(pid int) (bool, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		wrappedErr := fmt.Errorf("find process %d: %w", pid, err)
		slog.Error("status: find process failed", "err", wrappedErr, "pid", pid)
		return false, wrappedErr
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	wrappedErr := fmt.Errorf("signal process %d: %w", pid, err)
	slog.Error("status: signal process failed", "err", wrappedErr, "pid", pid)
	return false, wrappedErr
}

func runOPNsenseDaemonIsEnabled(args []string) int {
	fs := flag.NewFlagSet("is-enabled", flag.ContinueOnError)
	name := fs.String("name", "mwan_opnsense", "rc.d service name")
	rcSubr := fs.String("rc-subr", "/etc/rc.subr", "rc.subr path")
	quiet := fs.Bool("quiet", false, "suppress status output")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *name == "" {
		fmt.Fprintln(os.Stderr, "is-enabled: -name required")
		return 2
	}
	if *rcSubr == "" {
		fmt.Fprintln(os.Stderr, "is-enabled: -rc-subr path required")
		return 2
	}

	enabled, err := checkRCEnabled(*name, *rcSubr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan-opnsense is-enabled: %v\n", err)
		return 2
	}
	if enabled {
		if !*quiet {
			fmt.Fprintln(os.Stdout, "mwan-opnsense is enabled")
		}
		return 0
	}
	if !*quiet {
		fmt.Fprintln(os.Stderr, "mwan-opnsense is disabled")
	}
	return 1
}

func checkRCEnabled(name, rcSubr string) (bool, error) {
	script := strings.Join([]string{
		`rc_subr_path="$1"`,
		`service_name="$2"`,
		`if [ ! -r "${rc_subr_path}" ]; then exit 2; fi`,
		`. "${rc_subr_path}"`,
		`name="${service_name}"`,
		`rcvar="${name}_enable"`,
		`load_rc_config "${name}" >/dev/null 2>&1`,
		`checkyesno "${rcvar}"`,
	}, "\n")
	ctx, cancel := context.WithTimeout(context.Background(), rcEnabledCheckTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", script, "mwan-opnsense-is-enabled", rcSubr, name)
	output, err := command.CombinedOutput()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			return false, nil
		case 2:
			return false, fmt.Errorf("rc.subr not readable: %s", rcSubr)
		default:
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = "no output"
			}
			return false, fmt.Errorf("rc.subr check failed with exit %d: %s", exitErr.ExitCode(), message)
		}
	}
	slog.Error("is-enabled: rc.subr check failed", "err", err, "rc_subr", rcSubr)
	return false, fmt.Errorf("run rc.subr check: %w", err)
}
