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
	"goodkind.io/mwan/internal/chunkedstream"
	"goodkind.io/mwan/internal/opnsenseclient"
	"goodkind.io/mwan/internal/opnsensesvc"
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
	op := fs.String("op", "version",
		"RPC to call: version|read-config|xpath-get|xpath-set|xpath-delete|exec|deploy-status|deploy|revert|smoke")
	repeat := fs.Int("repeat", 1, "number of times to run the selected RPC over one connection")
	xpath := fs.String("xpath", "", "XPath expression for op=xpath-{get,set,delete}")
	xpathValue := fs.String("xpath-value", "", "value to write for op=xpath-set")
	cmdStr := fs.String("cmd", "", "command for op=exec")
	cmdArgs := fs.String("cmd-args", "", "comma-separated args for op=exec")
	cmdSudo := fs.Bool("cmd-sudo", false, "wrap exec in sudo -n")
	deployBin := fs.String("deploy-bin", "", "path to local binary for op=deploy (read into request)")
	deployVer := fs.String("deploy-version", "", "version label attached to op=deploy")
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

	probeArgs := probeRPCArgs{
		op:            *op,
		xpath:         *xpath,
		xpathValue:    *xpathValue,
		cmd:           *cmdStr,
		cmdArgs:       *cmdArgs,
		cmdSudo:       *cmdSudo,
		deployBin:     *deployBin,
		deployVersion: *deployVer,
	}
	if *op == "version" && *repeat == 1 {
		return runOPNsenseProbeRPC(ctx, cli.RPC(), probeArgs)
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
		if err := runOPNsenseProbeRPC(ctx, rpc, probeArgs); err != nil {
			return err
		}
	}
	return nil
}

func runOPNsenseProbeSmoke(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient) error {
	ops := []probeRPCArgs{
		{op: "version"},
		{op: "read-config"},
		{op: "xpath-get", xpath: "/opnsense/system/hostname"},
		{op: "exec", cmd: "uname", cmdArgs: "-s,-r,-m"},
		{op: "deploy-status"},
	}
	for _, smokeOp := range ops {
		fmt.Fprintf(os.Stdout, "smoke-op=%s\n", smokeOp.op)
		if err := runOPNsenseProbeRPC(ctx, rpc, smokeOp); err != nil {
			return err
		}
	}
	return nil
}

// probeRPCArgs bundles the op selector and per-op arguments accepted
// by opnsense-probe so the dispatcher signature stays small.
type probeRPCArgs struct {
	op            string
	xpath         string
	xpathValue    string
	cmd           string
	cmdArgs       string
	cmdSudo       bool
	deployBin     string
	deployVersion string
}

// probeOp is the typed enum of -op values accepted by opnsense-probe.
type probeOp string

const (
	probeOpVersion      probeOp = "version"
	probeOpReadConfig   probeOp = "read-config"
	probeOpXPathGet     probeOp = "xpath-get"
	probeOpXPathSet     probeOp = "xpath-set"
	probeOpXPathDelete  probeOp = "xpath-delete"
	probeOpExec         probeOp = "exec"
	probeOpDeployStatus probeOp = "deploy-status"
	probeOpDeploy       probeOp = "deploy"
	probeOpRevert       probeOp = "revert"
)

func runOPNsenseProbeRPC(
	ctx context.Context,
	rpc mwanv1.MWANOPNsenseServiceClient,
	a probeRPCArgs,
) error {
	switch probeOp(a.op) {
	case probeOpVersion:
		return probeVersion(ctx, rpc)
	case probeOpReadConfig:
		return probeReadConfig(ctx, rpc)
	case probeOpXPathGet:
		return probeXPathGet(ctx, rpc, a.xpath)
	case probeOpExec:
		return probeExec(ctx, rpc, a.cmd, a.cmdArgs, a.cmdSudo)
	case probeOpXPathSet:
		return probeXPathSet(ctx, rpc, a.xpath, a.xpathValue)
	case probeOpXPathDelete:
		return probeXPathDelete(ctx, rpc, a.xpath)
	case probeOpDeployStatus:
		return probeDeployStatus(ctx, rpc)
	case probeOpDeploy:
		return probeDeploy(ctx, rpc, a.deployBin, a.deployVersion)
	case probeOpRevert:
		return probeRevert(ctx, rpc)
	default:
		return fmt.Errorf("unknown op %q", a.op)
	}
}

func probeVersion(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient) error {
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
	return nil
}

func probeReadConfig(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient) error {
	resp, err := rpc.ReadConfigXML(ctx, &mwanv1.ReadConfigXMLRequest{})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe ReadConfigXML failed", "err", err)
		return fmt.Errorf("rpc ReadConfigXML: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe ReadConfigXML OK",
		"size_bytes", resp.GetSizeBytes(),
		"sha256", resp.GetSha256())
	fmt.Fprintf(os.Stdout, "size=%d sha256=%s\n", resp.GetSizeBytes(), resp.GetSha256())
	return nil
}

func probeXPathGet(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient, xpath string) error {
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
	return nil
}

func probeExec(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient,
	cmdStr, cmdArgs string, cmdSudo bool,
) error {
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
	return nil
}

func probeXPathSet(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient, xpath, value string) error {
	if xpath == "" {
		return errors.New("op=xpath-set requires -xpath")
	}
	resp, err := rpc.XPathSet(ctx, &mwanv1.XPathSetRequest{
		Expression: xpath,
		NewValue:   value,
	})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe XPathSet failed", "err", err)
		return fmt.Errorf("rpc XPathSet: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe XPathSet OK",
		"changed_count", resp.GetChangedCount(),
		"backup_path", resp.GetBackupPath())
	fmt.Fprintf(os.Stdout, "changed_count=%d backup=%s\n",
		resp.GetChangedCount(), resp.GetBackupPath())
	return nil
}

func probeXPathDelete(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient, xpath string) error {
	if xpath == "" {
		return errors.New("op=xpath-delete requires -xpath")
	}
	resp, err := rpc.XPathDelete(ctx, &mwanv1.XPathDeleteRequest{Expression: xpath})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe XPathDelete failed", "err", err)
		return fmt.Errorf("rpc XPathDelete: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe XPathDelete OK",
		"deleted_count", resp.GetDeletedCount(),
		"backup_path", resp.GetBackupPath())
	fmt.Fprintf(os.Stdout, "deleted_count=%d backup=%s\n",
		resp.GetDeletedCount(), resp.GetBackupPath())
	return nil
}

func probeDeployStatus(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient) error {
	resp, err := rpc.DeployStatus(ctx, &mwanv1.DeployStatusRequest{})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe DeployStatus failed", "err", err)
		return fmt.Errorf("rpc DeployStatus: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe DeployStatus OK",
		"active_sha256", resp.GetActiveSha256(),
		"previous_sha256", resp.GetPreviousSha256(),
		"health", resp.GetHealth(),
		"deployed_at", resp.GetDeployedAt())
	fmt.Fprintf(os.Stdout, "active=%s previous=%s health=%s deployed_at=%d\n",
		resp.GetActiveSha256(), resp.GetPreviousSha256(),
		resp.GetHealth(), resp.GetDeployedAt())
	return nil
}

func probeDeploy(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient,
	deployBin, deployVersion string,
) error {
	if deployBin == "" {
		return errors.New("op=deploy requires -deploy-bin")
	}
	file, err := os.Open(deployBin)
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe Deploy open failed",
			"path", deployBin, "err", err)
		return fmt.Errorf("open deploy-bin: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.WarnContext(ctx, "opnsense-probe Deploy close source failed",
				"path", deployBin, "err", closeErr)
		}
	}()
	info, statErr := file.Stat()
	if statErr != nil {
		return fmt.Errorf("stat deploy-bin: %w", statErr)
	}
	totalSize := info.Size()

	stream, err := rpc.Deploy(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe Deploy stream open failed", "err", err)
		return fmt.Errorf("rpc Deploy open: %w", err)
	}
	header := &mwanv1.ChunkHeader{
		ContentType: "application/octet-stream",
		Label:       deployVersion,
		TotalSize:   totalSize,
		Attrs: map[string]string{
			opnsensesvc.DeployAttrVersionStr: deployVersion,
		},
	}
	sumHex, sentBytes, sendErr := chunkedstream.Send(file, header, 0, stream.Send)
	if sendErr != nil {
		slog.ErrorContext(ctx, "opnsense-probe Deploy send failed", "err", sendErr)
		return fmt.Errorf("rpc Deploy send: %w", sendErr)
	}
	resp, recvErr := stream.CloseAndRecv()
	if recvErr != nil {
		slog.ErrorContext(ctx, "opnsense-probe Deploy CloseAndRecv failed", "err", recvErr)
		return fmt.Errorf("rpc Deploy reply: %w", recvErr)
	}
	slog.InfoContext(ctx, "opnsense-probe Deploy OK",
		"staged_sha256", resp.GetStagedSha256(),
		"previous_path", resp.GetPreviousPath(),
		"reexec_started", resp.GetReExecStarted(),
		"size_bytes", sentBytes,
		"client_sha256", sumHex)
	fmt.Fprintf(os.Stdout, "staged=%s previous_path=%s reexec_started=%v size_bytes=%d\n",
		resp.GetStagedSha256(), resp.GetPreviousPath(), resp.GetReExecStarted(), sentBytes)
	return nil
}

func probeRevert(ctx context.Context, rpc mwanv1.MWANOPNsenseServiceClient) error {
	resp, err := rpc.Revert(ctx, &mwanv1.RevertRequest{})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe Revert failed", "err", err)
		return fmt.Errorf("rpc Revert: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe Revert OK",
		"reverted_to_sha256", resp.GetRevertedToSha256(),
		"reexec_started", resp.GetReExecStarted())
	fmt.Fprintf(os.Stdout, "reverted_to=%s reexec_started=%v\n",
		resp.GetRevertedToSha256(), resp.GetReExecStarted())
	return nil
}
