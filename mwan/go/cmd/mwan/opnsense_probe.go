package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsenseclient"
)

const opnsenseProbeSerialSettleDelay = 1200 * time.Millisecond

// runOPNsenseProbe is the operational tool for ad hoc dialing of the
// mwan-opnsense daemon over its OOB virtio-serial unix socket.
//
// Example:
//
//	mwan opnsense-probe \
//	    -target unix:///var/run/qemu-server/101.mwanrpc \
//	    -op version
//
//	mwan opnsense-probe \
//	    -target unix:///var/run/qemu-server/101.mwanrpc \
//	    -op smoke
func runOPNsenseProbe(args []string) error {
	fs := flag.NewFlagSet("opnsense-probe", flag.ExitOnError)
	target := fs.String("target", "", "unix:///path/to/socket (required)")
	timeout := fs.Duration("timeout", 10*time.Second, "dial+RPC timeout")
	op := fs.String("op", "version", "RPC to call: version|read-config|xpath-get|exec|smoke")
	repeat := fs.Int("repeat", 1, "number of times to run the selected RPC over one connection")
	xpath := fs.String("xpath", "", "XPath expression for op=xpath-get")
	cmdStr := fs.String("cmd", "", "command for op=exec")
	cmdArgs := fs.String("cmd-args", "", "comma-separated args for op=exec")
	cmdSudo := fs.Bool("cmd-sudo", false, "wrap exec in sudo -n")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *target == "" {
		fs.Usage()
		return errors.New("-target required")
	}
	if !strings.HasPrefix(*target, "unix:///") {
		return fmt.Errorf("-target must be unix:///path; got %q", *target)
	}
	if *repeat < 1 {
		return errors.New("-repeat must be >= 1")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cli, err := opnsenseclient.Dial(ctx, opnsenseclient.Config{
		Target:      *target,
		DialTimeout: *timeout,
	})
	if err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()

	if *op == "version" && *repeat == 1 {
		return runOPNsenseProbeRPC(ctx, cli.RPC(), *op, *xpath, *cmdStr, *cmdArgs, *cmdSudo)
	}
	timer := time.NewTimer(opnsenseProbeSerialSettleDelay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
	}

	rpc := cli.RPC()
	for i := 1; i <= *repeat; i++ {
		if *repeat > 1 {
			fmt.Fprintf(os.Stdout, "repeat=%d/%d\n", i, *repeat)
		}
		if *op == "smoke" {
			if err := runOPNsenseProbeSmoke(ctx, rpc); err != nil {
				return err
			}
			continue
		}
		if err := runOPNsenseProbeRPC(ctx, rpc, *op, *xpath, *cmdStr, *cmdArgs, *cmdSudo); err != nil {
			return err
		}
	}
	return nil
}

func runOPNsenseProbeSmoke(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient) error {
	ops := []struct {
		op      string
		xpath   string
		cmd     string
		cmdArgs string
	}{
		{op: "version"},
		{op: "read-config"},
		{op: "xpath-get", xpath: "/opnsense/system/hostname"},
		{op: "exec", cmd: "uname", cmdArgs: "-s,-r,-m"},
	}
	for _, smokeOp := range ops {
		fmt.Fprintf(os.Stdout, "smoke-op=%s\n", smokeOp.op)
		if err := runOPNsenseProbeRPC(ctx, rpc, smokeOp.op, smokeOp.xpath, smokeOp.cmd, smokeOp.cmdArgs, false); err != nil {
			return err
		}
	}
	return nil
}

// probeOp is the typed enum of -op values accepted by opnsense-probe.
type probeOp string

const (
	probeOpVersion    probeOp = "version"
	probeOpReadConfig probeOp = "read-config"
	probeOpXPathGet   probeOp = "xpath-get"
	probeOpExec       probeOp = "exec"
)

func runOPNsenseProbeRPC(
	ctx context.Context,
	rpc mwanv1.MWANOPNsenseServiceClient,
	op string,
	xpath string,
	cmdStr string,
	cmdArgs string,
	cmdSudo bool,
) error {
	switch probeOp(op) {
	case probeOpVersion:
		resp, err := rpc.Version(ctx, &mwanv1.VersionRequest{})
		if err != nil {
			slog.ErrorContext(ctx, "opnsense-probe Version failed", "err", err)
			return fmt.Errorf("rpc Version: %w", err)
		}
		slog.InfoContext(ctx, "opnsense-probe Version OK",
			"version", resp.GetVersion(),
			"commit", resp.GetBuildCommit(),
			"dirty", resp.GetBuildDirty(),
			"binhash", resp.GetBuildBinhash())
		fmt.Fprintf(os.Stdout, "version=%s commit=%s dirty=%v binhash=%s\n",
			resp.GetVersion(), resp.GetBuildCommit(), resp.GetBuildDirty(), resp.GetBuildBinhash())
	case probeOpReadConfig:
		resp, err := rpc.ReadConfigXML(ctx, &mwanv1.ReadConfigXMLRequest{})
		if err != nil {
			slog.ErrorContext(ctx, "opnsense-probe ReadConfigXML failed", "err", err)
			return fmt.Errorf("rpc ReadConfigXML: %w", err)
		}
		slog.InfoContext(ctx, "opnsense-probe ReadConfigXML OK",
			"size_bytes", resp.GetSizeBytes(),
			"sha256", resp.GetSha256())
		fmt.Fprintf(os.Stdout, "size=%d sha256=%s\n", resp.GetSizeBytes(), resp.GetSha256())
	case probeOpXPathGet:
		if xpath == "" {
			return errors.New("op=xpath-get requires -xpath")
		}
		resp, err := rpc.XPathGet(ctx, &mwanv1.XPathGetRequest{Expression: xpath})
		if err != nil {
			slog.ErrorContext(ctx, "opnsense-probe XPathGet failed", "err", err)
			return fmt.Errorf("rpc XPathGet: %w", err)
		}
		slog.InfoContext(ctx, "opnsense-probe XPathGet OK", "matches", len(resp.GetMatches()))
		for _, m := range resp.GetMatches() {
			fmt.Fprintln(os.Stdout, m)
		}
	case probeOpExec:
		if cmdStr == "" {
			return errors.New("op=exec requires -cmd")
		}
		var argv []string
		if cmdArgs != "" {
			argv = strings.Split(cmdArgs, ",")
		}
		resp, err := rpc.Exec(ctx, &mwanv1.ExecRequest{
			Command: cmdStr,
			Args:    argv,
			Sudo:    cmdSudo,
		})
		if err != nil {
			slog.ErrorContext(ctx, "opnsense-probe Exec failed", "err", err)
			return fmt.Errorf("rpc Exec: %w", err)
		}
		slog.InfoContext(ctx, "opnsense-probe Exec OK",
			"exit_code", resp.GetExitCode(),
			"duration_ms", resp.GetDurationMs(),
			"stdout_truncated", resp.GetStdoutTruncated(),
			"stderr_truncated", resp.GetStderrTruncated(),
			"timed_out", resp.GetTimedOut())
		_, _ = os.Stdout.Write(resp.GetStdout())
		_, _ = os.Stderr.Write(resp.GetStderr())
		if resp.GetExitCode() != 0 {
			return fmt.Errorf("remote exit code %d", resp.GetExitCode())
		}
	default:
		return fmt.Errorf("unknown op %q", op)
	}
	return nil
}
