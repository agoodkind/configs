package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"goodkind.io/mwan/internal/opnsensesvc"
)

const (
	rcEnabledCheckTimeout = 5 * time.Second

	// defaultSerialBaud is the kernel's hard cap on virtio-serial baud
	// on FreeBSD. Higher values are silently clamped. This sets the tty
	// input/output queue size at c_ispeed/5 = 23040 bytes, which is the
	// headroom each MWN1 streaming chunk fits in.
	defaultSerialBaud = uint32(115200)

	// defaultSerialPath is the FreeBSD virtio-console node the host-side
	// chardev is wired to. The Proxmox VM template pins this; if you
	// change the chardev order in qemu, also change this constant.
	defaultSerialPath = "/dev/ttyV0.1"

	// defaultRCName / defaultRCSubr are the rc.d service name and the
	// rc.subr path the is-enabled check resolves. Both live on FreeBSD
	// hosts and have stable conventional paths.
	defaultRCName = "mwan_opnsense"
	defaultRCSubr = "/etc/rc.subr"
)

// runOPNsenseDaemonServe starts the MWN1 dispatcher daemon with the
// virtio-serial-pci listener. There is exactly one listener and exactly
// one peer; auth is unix socket permissions on the host side (root-only)
// so the daemon does not authenticate at the application layer.
//
// After MWAN-191, the serve verb takes no flags. The serial path, baud,
// config path, and backup dir are compiled-in constants today and will
// move to the daemon-side TOML in MWAN-193. The verb still accepts an
// empty arg slice or a help token for forward compatibility.
func runOPNsenseDaemonServe(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			fmt.Fprintln(os.Stdout, "usage: mwan opnsense daemon serve")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Run the in-VM dispatcher daemon. No flags; config comes from")
			fmt.Fprintln(os.Stdout, "compiled defaults today and from daemon-side TOML in MWAN-193.")
			return 0
		}
	}
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "mwan opnsense daemon serve: unexpected arguments: %v\n", args)
		return 2
	}

	serialPath := defaultSerialPath
	configPath := opnsensesvc.ConfigPath
	backupDir := opnsensesvc.BackupDir
	baud := defaultSerialBaud

	logger := slog.Default()
	srv := opnsensesvc.NewServer(logger, configPath, backupDir)
	validator := opnsensesvc.NewPathValidator(logger, opnsensesvc.DefaultReadAllowlist, opnsensesvc.DefaultWriteAllowlist)
	transferMgr, transferErr := opnsensesvc.NewTransferManager(logger, validator, opnsensesvc.TransferStateDir, nil)
	if transferErr != nil {
		fmt.Fprintf(os.Stderr, "daemon serve: build transfer manager: %v\n", transferErr)
		return 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	// We do not `defer cancel()` because we explicitly call it before
	// returning from this subcommand.
	srv.SetRestartHook(func() {
		logger.Info("mwan-opnsense: RestartDaemon hook firing, cancelling serve ctx")
		cancel()
	})

	openSerial := func(path string) (io.ReadWriteCloser, error) {
		return opnsensesvc.OpenVirtioSerial(path, baud, logger)
	}

	opts := opnsensesvc.ServeOpts{
		SerialPath:   serialPath,
		OpenSerial:   openSerial,
		Server:       srv,
		Log:          logger,
		OnSerialOpen: nil,
		OnGRPCAccept: nil,
		Transfer:     transferMgr,
		StopTimeout:  0,
	}

	slog.Info("mwan-opnsense: serving",
		"serial_path", serialPath,
		"baud", baud)

	serveErr := opnsensesvc.Serve(ctx, opts)
	cancel()
	if serveErr != nil {
		slog.Error("daemon serve: terminated", "err", serveErr)
		return 1
	}
	slog.Info("mwan-opnsense: stopped")
	return 0
}

// runOPNsenseDaemonIsEnabled returns exit 0 when the rc.d service is
// enabled in /etc/rc.conf, 1 when it is disabled, 2 on error. No flags;
// the name and rc.subr path are compiled constants.
func runOPNsenseDaemonIsEnabled(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			fmt.Fprintln(os.Stdout, "usage: mwan opnsense daemon is-enabled")
			return 0
		}
	}
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "mwan opnsense daemon is-enabled: unexpected arguments: %v\n", args)
		return 2
	}
	enabled, err := checkRCEnabled(defaultRCName, defaultRCSubr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan opnsense daemon is-enabled: %v\n", err)
		return 2
	}
	if enabled {
		fmt.Fprintln(os.Stdout, "mwan-opnsense is enabled")
		return 0
	}
	fmt.Fprintln(os.Stderr, "mwan-opnsense is disabled")
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
	slog.Error("daemon is-enabled: rc.subr check failed", "err", err, "rc_subr", rcSubr)
	return false, fmt.Errorf("run rc.subr check: %w", err)
}
