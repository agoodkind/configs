package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsenseclient"
)

// runOPNsenseProbe is the operational tool for ad hoc dialing of the
// mwan-opnsense daemon. It exercises the same gRPC client path that
// cutover2 will use, so a successful probe is also a smoke test of
// the production code path.
//
// Examples:
//
//	mwan opnsense-probe \
//	    -target tcp://[3d06:bad:b01:fe::2]:9443 \
//	    -cert vault.crt -key vault.key -ca ca.crt \
//	    -authority opnsense-test
//
//	mwan opnsense-probe \
//	    -target unix:///var/run/qemu-server/101.mwanrpc \
//	    -cert vault.crt -key vault.key -ca ca.crt \
//	    -authority opnsense-test
func runOPNsenseProbe(args []string) error {
	fs := flag.NewFlagSet("opnsense-probe", flag.ExitOnError)
	target := fs.String("target", "", "tcp://host:port or unix:///path/to/socket (required)")
	certPath := fs.String("cert", "", "client cert PEM (required)")
	keyPath := fs.String("key", "", "client key PEM (required)")
	caPath := fs.String("ca", "", "CA cert PEM (required)")
	authority := fs.String("authority", "", "override :authority pseudo-header (defaults to dial host; use 'localhost' for unix)")
	timeout := fs.Duration("timeout", 10*time.Second, "dial+RPC timeout")
	op := fs.String("op", "version", "RPC to call: version|read-config|xpath-get|exec")
	xpath := fs.String("xpath", "", "XPath expression for op=xpath-get")
	cmdStr := fs.String("cmd", "", "command for op=exec")
	cmdArgs := fs.String("cmd-args", "", "comma-separated args for op=exec")
	cmdSudo := fs.Bool("cmd-sudo", false, "wrap exec in sudo -n")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *target == "" || *certPath == "" || *keyPath == "" || *caPath == "" {
		fs.Usage()
		return errors.New("-target, -cert, -key, -ca all required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cli, err := opnsenseclient.Dial(ctx, opnsenseclient.Config{
		Target:      *target,
		CertPath:    *certPath,
		KeyPath:     *keyPath,
		CAPath:      *caPath,
		Authority:   *authority,
		DialTimeout: *timeout,
	})
	if err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()

	rpc := cli.RPC()
	switch *op {
	case "version":
		resp, err := rpc.Version(ctx, &mwanv1.VersionRequest{})
		if err != nil {
			return fmt.Errorf("rpc Version: %w", err)
		}
		slog.Info("opnsense-probe Version OK",
			"version", resp.GetVersion(),
			"commit", resp.GetBuildCommit(),
			"dirty", resp.GetBuildDirty(),
			"binhash", resp.GetBuildBinhash())
		fmt.Fprintf(os.Stdout, "version=%s commit=%s dirty=%v binhash=%s\n",
			resp.GetVersion(), resp.GetBuildCommit(), resp.GetBuildDirty(), resp.GetBuildBinhash())
	case "read-config":
		resp, err := rpc.ReadConfigXML(ctx, &mwanv1.ReadConfigXMLRequest{})
		if err != nil {
			return fmt.Errorf("rpc ReadConfigXML: %w", err)
		}
		slog.Info("opnsense-probe ReadConfigXML OK",
			"size_bytes", resp.GetSizeBytes(),
			"sha256", resp.GetSha256())
		fmt.Fprintf(os.Stdout, "size=%d sha256=%s\n", resp.GetSizeBytes(), resp.GetSha256())
	case "xpath-get":
		if *xpath == "" {
			return errors.New("op=xpath-get requires -xpath")
		}
		resp, err := rpc.XPathGet(ctx, &mwanv1.XPathGetRequest{Expression: *xpath})
		if err != nil {
			return fmt.Errorf("rpc XPathGet: %w", err)
		}
		slog.Info("opnsense-probe XPathGet OK", "matches", len(resp.GetMatches()))
		for _, m := range resp.GetMatches() {
			fmt.Fprintln(os.Stdout, m)
		}
	case "exec":
		if *cmdStr == "" {
			return errors.New("op=exec requires -cmd")
		}
		var argv []string
		if *cmdArgs != "" {
			argv = splitCSVProbe(*cmdArgs)
		}
		resp, err := rpc.Exec(ctx, &mwanv1.ExecRequest{
			Command: *cmdStr,
			Args:    argv,
			Sudo:    *cmdSudo,
		})
		if err != nil {
			return fmt.Errorf("rpc Exec: %w", err)
		}
		slog.Info("opnsense-probe Exec OK",
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
		return fmt.Errorf("unknown op %q", *op)
	}
	return nil
}

func splitCSVProbe(s string) []string {
	out := []string{}
	cur := []byte{}
	for i := range len(s) {
		if s[i] == ',' {
			out = append(out, string(cur))
			cur = cur[:0]
			continue
		}
		cur = append(cur, s[i])
	}
	out = append(out, string(cur))
	return out
}
