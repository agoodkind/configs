package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsense"
	"goodkind.io/mwan/internal/opnsensesvc"
)

const (
	opnsenseProbeSerialSettleDelay = 1200 * time.Millisecond
	opnsenseProbeDeployChunkBytes  = 8 * 1024
	opnsenseProbeConfigctlCommand  = "/usr/local/sbin/configctl"
	configctlActionNotAllowedText  = "Action not allowed or missing"
	maxProbeCommandTimeoutSeconds  = int32(300)
)

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
		"RPC to call: version|read-config|write-config|backup-config|xpath-get|xpath-set|xpath-delete|exec|configctl|strip-gatewayv6|inject-gatewayv6|deploy-status|deploy|revert|smoke")
	repeat := fs.Int("repeat", 1, "number of times to run the selected RPC over one connection")
	xpath := fs.String("xpath", "", "XPath expression for op=xpath-{get,set,delete}")
	xpathValue := fs.String("xpath-value", "", "value to write for op=xpath-set")
	cmdStr := fs.String("cmd", "", "executable path for op=exec (argv token, not a shell string; no shell expansion or pipes)")
	cmdArgs := fs.String("cmd-args", "", "comma-separated argv tokens for op=exec (legacy; prefer -cmd-arg)")
	var cmdArgv repeatableStringFlag
	fs.Var(&cmdArgv, "cmd-arg", "append one argv token for op=exec; repeatable and preferred over -cmd-args; for shell pipelines use -cmd /bin/sh -cmd-arg -c -cmd-arg \"cmd || fallback\"")
	cmdStdinFile := fs.String("stdin-file", "", "local file to send as stdin for op=exec")
	cmdTimeout := fs.Duration("cmd-timeout", 0, "remote command timeout for op=exec or op=configctl")
	cmdSudo := fs.Bool("cmd-sudo", false, "wrap exec in sudo -n")
	configXML := fs.String("config-xml", "", "path to XML content for op=write-config")
	label := fs.String("label", "", "backup label for op=write-config or op=backup-config")
	gatewayName := fs.String("gateway-name", "", "gateway name for op=inject-gatewayv6")
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
	if len(fs.Args()) > 0 && *op != string(probeOpConfigctl) {
		return fmt.Errorf("unexpected positional args for op=%s: %s", *op, strings.Join(fs.Args(), " "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cli, err := opnsense.Dial(*target)
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe dial failed", "target", *target, "err", err)
		return fmt.Errorf("dial opnsense target: %w", err)
	}
	defer func() { _ = cli.Close() }()

	probeArgs := probeRPCArgs{
		op:            *op,
		xpath:         *xpath,
		xpathValue:    *xpathValue,
		cmd:           *cmdStr,
		cmdArgs:       *cmdArgs,
		cmdArgv:       []string(cmdArgv),
		cmdStdinFile:  *cmdStdinFile,
		cmdTimeout:    *cmdTimeout,
		cmdSudo:       *cmdSudo,
		configctlArgs: fs.Args(),
		configXML:     *configXML,
		label:         *label,
		gatewayName:   *gatewayName,
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
	case <-cli.Done():
		timer.Stop()
		if err := cli.Err(); err != nil {
			slog.ErrorContext(ctx, "opnsense-probe connection closed during settle", "err", err)
			return fmt.Errorf("opnsense connection closed during settle: %w", err)
		}
		return opnsense.ErrClientClosed
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

func runOPNsenseProbeSmoke(ctx context.Context, rpc *opnsense.RPC) error {
	ops := []probeRPCArgs{
		{op: "version"},
		{op: "read-config"},
		{op: "xpath-get", xpath: "/opnsense/system/hostname"},
		{op: "exec", cmd: "uname", cmdArgv: []string{"-s", "-r", "-m"}},
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
	cmdArgv       []string
	cmdStdinFile  string
	cmdTimeout    time.Duration
	cmdSudo       bool
	configctlArgs []string
	configXML     string
	label         string
	gatewayName   string
	deployBin     string
	deployVersion string
}

type repeatableStringFlag []string

func (f *repeatableStringFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *repeatableStringFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// probeOp is the typed enum of -op values accepted by opnsense-probe.
type probeOp string

const (
	probeOpVersion      probeOp = "version"
	probeOpReadConfig   probeOp = "read-config"
	probeOpWriteConfig  probeOp = "write-config"
	probeOpBackupConfig probeOp = "backup-config"
	probeOpXPathGet     probeOp = "xpath-get"
	probeOpXPathSet     probeOp = "xpath-set"
	probeOpXPathDelete  probeOp = "xpath-delete"
	probeOpExec         probeOp = "exec"
	probeOpConfigctl    probeOp = "configctl"
	probeOpStripGW6     probeOp = "strip-gatewayv6"
	probeOpInjectGW6    probeOp = "inject-gatewayv6"
	probeOpDeployStatus probeOp = "deploy-status"
	probeOpDeploy       probeOp = "deploy"
	probeOpRevert       probeOp = "revert"
)

func runOPNsenseProbeRPC(
	ctx context.Context,
	rpc *opnsense.RPC,
	a probeRPCArgs,
) error {
	switch probeOp(a.op) {
	case probeOpVersion:
		return probeVersion(ctx, rpc)
	case probeOpReadConfig:
		return probeReadConfig(ctx, rpc)
	case probeOpWriteConfig:
		return probeWriteConfig(ctx, rpc, a.configXML, a.label)
	case probeOpBackupConfig:
		return probeBackupConfig(ctx, rpc, a.label)
	case probeOpXPathGet:
		return probeXPathGet(ctx, rpc, a.xpath)
	case probeOpExec:
		return probeExec(ctx, rpc, a)
	case probeOpConfigctl:
		return probeConfigctl(ctx, rpc, a.configctlArgs, a.cmdTimeout)
	case probeOpXPathSet:
		return probeXPathSet(ctx, rpc, a.xpath, a.xpathValue)
	case probeOpXPathDelete:
		return probeXPathDelete(ctx, rpc, a.xpath)
	case probeOpStripGW6:
		return probeStripGatewayV6(ctx, rpc)
	case probeOpInjectGW6:
		return probeInjectGatewayV6(ctx, rpc, a.gatewayName)
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

func probeVersion(ctx context.Context, rpc *opnsense.RPC) error {
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

func probeReadConfig(ctx context.Context, rpc *opnsense.RPC) error {
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

func probeWriteConfig(ctx context.Context, rpc *opnsense.RPC, path string, label string) error {
	if path == "" {
		return errors.New("op=write-config requires -config-xml")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe WriteConfigXML read source failed",
			"path", path, "err", err)
		return fmt.Errorf("read config-xml: %w", err)
	}
	resp, err := rpc.WriteConfigXML(ctx, &mwanv1.WriteConfigXMLRequest{
		Content: content,
		Label:   label,
	})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe WriteConfigXML failed", "err", err)
		return fmt.Errorf("rpc WriteConfigXML: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe WriteConfigXML OK",
		"backup_path", resp.GetBackupPath(),
		"bytes_written", resp.GetBytesWritten())
	fmt.Fprintf(os.Stdout, "backup=%s bytes_written=%d\n",
		resp.GetBackupPath(), resp.GetBytesWritten())
	return nil
}

func probeBackupConfig(ctx context.Context, rpc *opnsense.RPC, label string) error {
	resp, err := rpc.BackupConfigXML(ctx, &mwanv1.BackupConfigXMLRequest{Label: label})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe BackupConfigXML failed", "err", err)
		return fmt.Errorf("rpc BackupConfigXML: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe BackupConfigXML OK",
		"backup_path", resp.GetBackupPath(),
		"size_bytes", resp.GetSizeBytes())
	fmt.Fprintf(os.Stdout, "backup=%s size=%d\n",
		resp.GetBackupPath(), resp.GetSizeBytes())
	return nil
}

func probeXPathGet(ctx context.Context, rpc *opnsense.RPC, xpath string) error {
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

func probeExec(ctx context.Context, rpc *opnsense.RPC, a probeRPCArgs) error {
	req, err := buildProbeExecRequest(a)
	if err != nil {
		return err
	}
	resp, err := rpc.Exec(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe Exec failed", "err", err)
		return fmt.Errorf("rpc Exec: %w", err)
	}
	return printAndValidateProbeExecResponse(ctx, "Exec", resp)
}

func probeConfigctl(
	ctx context.Context,
	rpc *opnsense.RPC,
	action []string,
	cmdTimeout time.Duration,
) error {
	req, reqErr := buildProbeConfigctlRequest(action, cmdTimeout)
	if reqErr != nil {
		return reqErr
	}
	resp, err := rpc.Exec(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe configctl failed", "err", err)
		return fmt.Errorf("rpc Exec configctl: %w", err)
	}
	return printAndValidateProbeExecResponse(ctx, "configctl", resp)
}

func buildProbeExecRequest(a probeRPCArgs) (*mwanv1.ExecRequest, error) {
	if a.cmd == "" {
		return nil, errors.New("op=exec requires -cmd")
	}
	stdinBytes, err := ReadProbeStdinFile(a.cmdStdinFile)
	if err != nil {
		return nil, err
	}
	return &mwanv1.ExecRequest{
		Command:        a.cmd,
		Args:           buildProbeExecArgv(a.cmdArgv, a.cmdArgs),
		Sudo:           a.cmdSudo,
		TimeoutSeconds: durationSeconds(a.cmdTimeout),
		StdinBytes:     stdinBytes,
	}, nil
}

func buildProbeConfigctlRequest(
	action []string,
	cmdTimeout time.Duration,
) (*mwanv1.ExecRequest, error) {
	if len(action) == 0 {
		return nil, errors.New("op=configctl requires action tokens after flags, e.g. -op configctl system event config_changed")
	}
	return &mwanv1.ExecRequest{
		Command:        opnsenseProbeConfigctlCommand,
		Args:           append([]string(nil), action...),
		TimeoutSeconds: durationSeconds(cmdTimeout),
	}, nil
}

func buildProbeExecArgv(cmdArgv []string, legacyCmdArgs string) []string {
	if len(cmdArgv) > 0 {
		return append([]string(nil), cmdArgv...)
	}
	if legacyCmdArgs == "" {
		return nil
	}
	return strings.Split(legacyCmdArgs, ",")
}

// ReadProbeStdinFile loads optional stdin bytes for op=exec.
func ReadProbeStdinFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	stdinBytes, err := os.ReadFile(path)
	if err != nil {
		slog.Error("opnsense-probe stdin-file read failed",
			"path", path, "err", err)
		return nil, fmt.Errorf("read stdin-file: %w", err)
	}
	return stdinBytes, nil
}

func durationSeconds(timeout time.Duration) int32 {
	if timeout <= 0 {
		return 0
	}
	remaining := timeout
	var seconds int32
	for remaining > 0 && seconds < maxProbeCommandTimeoutSeconds {
		seconds++
		remaining -= time.Second
	}
	return seconds
}

func printAndValidateProbeExecResponse(
	ctx context.Context,
	opName string,
	resp *mwanv1.ExecResponse,
) error {
	slog.InfoContext(ctx, "opnsense-probe Exec OK",
		"op", opName,
		"exit_code", resp.GetExitCode(),
		"duration_ms", resp.GetDurationMs(),
		"stdout_truncated", resp.GetStdoutTruncated(),
		"stderr_truncated", resp.GetStderrTruncated(),
		"timed_out", resp.GetTimedOut())
	_, _ = os.Stdout.Write(resp.GetStdout())
	_, _ = os.Stderr.Write(resp.GetStderr())
	return validateProbeExecResponse(opName, resp)
}

func validateProbeExecResponse(opName string, resp *mwanv1.ExecResponse) error {
	if responseContainsConfigctlActionFailure(resp) {
		return fmt.Errorf("remote %s reported %q", opName, configctlActionNotAllowedText)
	}
	if resp.GetExitCode() != 0 {
		return fmt.Errorf("remote exit code %d", resp.GetExitCode())
	}
	return nil
}

func responseContainsConfigctlActionFailure(resp *mwanv1.ExecResponse) bool {
	if resp == nil {
		return false
	}
	return strings.Contains(string(resp.GetStdout()), configctlActionNotAllowedText) ||
		strings.Contains(string(resp.GetStderr()), configctlActionNotAllowedText)
}

func probeXPathSet(ctx context.Context, rpc *opnsense.RPC, xpath, value string) error {
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

func probeXPathDelete(ctx context.Context, rpc *opnsense.RPC, xpath string) error {
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

func probeStripGatewayV6(ctx context.Context, rpc *opnsense.RPC) error {
	resp, err := rpc.StripGatewayV6(ctx, &mwanv1.StripGatewayV6Request{})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe StripGatewayV6 failed", "err", err)
		return fmt.Errorf("rpc StripGatewayV6: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe StripGatewayV6 OK",
		"backup_path", resp.GetBackupPath(),
		"changed", resp.GetChanged())
	fmt.Fprintf(os.Stdout, "backup=%s changed=%v\n",
		resp.GetBackupPath(), resp.GetChanged())
	return nil
}

func probeInjectGatewayV6(ctx context.Context, rpc *opnsense.RPC, gatewayName string) error {
	if gatewayName == "" {
		return errors.New("op=inject-gatewayv6 requires -gateway-name")
	}
	resp, err := rpc.InjectGatewayV6(ctx, &mwanv1.InjectGatewayV6Request{GatewayName: gatewayName})
	if err != nil {
		slog.ErrorContext(ctx, "opnsense-probe InjectGatewayV6 failed", "err", err)
		return fmt.Errorf("rpc InjectGatewayV6: %w", err)
	}
	slog.InfoContext(ctx, "opnsense-probe InjectGatewayV6 OK",
		"backup_path", resp.GetBackupPath(),
		"changed", resp.GetChanged())
	fmt.Fprintf(os.Stdout, "backup=%s changed=%v\n",
		resp.GetBackupPath(), resp.GetChanged())
	return nil
}

func probeDeployStatus(ctx context.Context, rpc *opnsense.RPC) error {
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

func probeDeploy(ctx context.Context, rpc *opnsense.RPC,
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
	defer stream.Cancel()
	header := &mwanv1.ChunkHeader{
		ContentType: "application/octet-stream",
		Label:       deployVersion,
		TotalSize:   totalSize,
		Attrs: map[string]string{
			opnsensesvc.DeployAttrVersionStr: deployVersion,
		},
	}
	sumHex, sentBytes, sendErr := sendDeployChunks(file, header, stream.Send)
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

func sendDeployChunks(
	reader io.Reader,
	header *mwanv1.ChunkHeader,
	send func(*mwanv1.Chunk) error,
) (string, int64, error) {
	if reader == nil {
		return "", 0, errors.New("deploy chunk source required")
	}
	if send == nil {
		return "", 0, errors.New("deploy chunk sender required")
	}
	headerChunk := &mwanv1.Chunk{Body: &mwanv1.Chunk_Header{Header: header}}
	if err := send(headerChunk); err != nil {
		slog.Error("opnsense-probe Deploy header send failed", "err", err)
		return "", 0, fmt.Errorf("send deploy header: %w", err)
	}

	hash := sha256.New()
	buffer := make([]byte, opnsenseProbeDeployChunkBytes)
	var totalBytes int64
	for {
		bytesRead, readErr := io.ReadFull(reader, buffer)
		if bytesRead > 0 {
			data := buffer[:bytesRead]
			if _, err := hash.Write(data); err != nil {
				slog.Error("opnsense-probe Deploy hash failed", "err", err)
				return "", 0, fmt.Errorf("hash deploy chunk: %w", err)
			}
			payload := make([]byte, bytesRead)
			copy(payload, data)
			chunk := &mwanv1.Chunk{Body: &mwanv1.Chunk_Data{Data: payload}}
			if err := send(chunk); err != nil {
				slog.Error("opnsense-probe Deploy data send failed",
					"err", err, "bytes", bytesRead)
				return "", 0, fmt.Errorf("send deploy data: %w", err)
			}
			totalBytes += int64(bytesRead)
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
		if readErr != nil {
			slog.Error("opnsense-probe Deploy source read failed",
				"err", readErr, "bytes_so_far", totalBytes)
			return "", 0, fmt.Errorf("read deploy source: %w", readErr)
		}
	}

	sumHex := hex.EncodeToString(hash.Sum(nil))
	trailer := &mwanv1.Chunk{Body: &mwanv1.Chunk_Trailer{Trailer: &mwanv1.ChunkTrailer{
		Sha256Hex: sumHex,
		TotalSize: totalBytes,
	}}}
	if err := send(trailer); err != nil {
		slog.Error("opnsense-probe Deploy trailer send failed", "err", err)
		return "", 0, fmt.Errorf("send deploy trailer: %w", err)
	}
	return sumHex, totalBytes, nil
}

func probeRevert(ctx context.Context, rpc *opnsense.RPC) error {
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
