package main

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/opnsense"
)

const (
	probeDefaultTimeout       = 10 * time.Second
	probeDefaultUploadChunk   = 16 * 1024
	probeMaxCommandTimeoutSec = int32(300)
)

// probeOp is a named enum type for the -op flag so the dispatch
// switch does not lint as "switch on bare string with many literal
// cases".
type probeOp string

const (
	opVersion         probeOp = "version"
	opReadConfig      probeOp = "read-config"
	opWriteConfig     probeOp = "write-config"
	opBackupConfig    probeOp = "backup-config"
	opXPathGet        probeOp = "xpath-get"
	opXPathSet        probeOp = "xpath-set"
	opXPathDelete     probeOp = "xpath-delete"
	opExec            probeOp = "exec"
	opStripGatewayV6  probeOp = "strip-gatewayv6"
	opInjectGatewayV6 probeOp = "inject-gatewayv6"
	opDeployStatus    probeOp = "deploy-status"
	opRevert          probeOp = "revert"
	opSelftest        probeOp = "selftest"
	opTransferUp      probeOp = "transfer-up"
	opTransferDown    probeOp = "transfer-down"
	opTransferGC      probeOp = "transfer-gc"
	opDeploy          probeOp = "deploy"
)

type probeFlags struct {
	target         string
	timeout        time.Duration
	op             string
	xpath          string
	xpathValue     string
	cmd            string
	cmdArgs        string
	cmdSudo        bool
	cmdTimeout     time.Duration
	configXML      string
	label          string
	gatewayName    string
	selftestSize   int
	transferPath   string
	transferSize   int64
	deployBinary   string
	transferSource string
	transferResume bool
	transferID     string
}

// parseProbeFlags binds every probe flag onto a FlagSet so the
// dispatch function below can remain small enough for funlen.
func parseProbeFlags(args []string) (*probeFlags, error) {
	fs := flag.NewFlagSet("opnsense-probe", flag.ExitOnError)
	pf := &probeFlags{
		target: "", timeout: 0, op: "", xpath: "", xpathValue: "",
		cmd: "", cmdArgs: "", cmdSudo: false, cmdTimeout: 0,
		configXML: "", label: "", gatewayName: "",
		selftestSize: 0, transferPath: "", transferSize: 0, deployBinary: "",
		transferSource: "", transferResume: false, transferID: "",
	}
	fs.StringVar(&pf.target, "target", "", "unix:///path/to/socket (required)")
	fs.DurationVar(&pf.timeout, "timeout", probeDefaultTimeout, "dial+RPC timeout")
	fs.StringVar(&pf.op, "op", "version",
		"RPC: version|read-config|write-config|exec|xpath-get|xpath-set|xpath-delete|backup-config|strip-gatewayv6|inject-gatewayv6|deploy-status|deploy|revert|selftest|transfer-up|transfer-down|transfer-gc")
	fs.StringVar(&pf.xpath, "xpath", "", "XPath expression for op=xpath-{get,set,delete}")
	fs.StringVar(&pf.xpathValue, "xpath-value", "", "value to write for op=xpath-set")
	fs.StringVar(&pf.cmd, "cmd", "", "executable path for op=exec")
	fs.StringVar(&pf.cmdArgs, "cmd-args", "", "comma-separated argv tokens for op=exec")
	fs.BoolVar(&pf.cmdSudo, "cmd-sudo", false, "wrap exec in sudo -n")
	fs.DurationVar(&pf.cmdTimeout, "cmd-timeout", 0, "remote command timeout for op=exec")
	fs.StringVar(&pf.configXML, "config-xml", "", "path to XML content for op=write-config")
	fs.StringVar(&pf.label, "label", "", "backup label for op=write-config or op=backup-config")
	fs.StringVar(&pf.gatewayName, "gateway-name", "", "gateway name for op=inject-gatewayv6")
	fs.IntVar(&pf.selftestSize, "selftest-size", 0, "byte count for op=selftest payload")
	fs.StringVar(&pf.transferPath, "path", "", "remote path for op=transfer-up|transfer-down")
	fs.Int64Var(&pf.transferSize, "size", 0, "size in bytes for op=transfer-up")
	fs.StringVar(&pf.deployBinary, "binary", "", "local binary path for op=deploy")
	fs.StringVar(&pf.transferSource, "source", "",
		"local source file for op=transfer-up; required when -resume is set")
	fs.BoolVar(&pf.transferResume, "resume", false,
		"resume the prior transfer for -source from its persisted offset")
	fs.StringVar(&pf.transferID, "transfer-id", "",
		"explicit transfer id when multiple resumable transfers share a source")
	if err := fs.Parse(args); err != nil {
		slog.ErrorContext(context.Background(), "opnsense-probe: parse", "err", err)
		return nil, fmt.Errorf("opnsense-probe: parse: %w", err)
	}
	if pf.target == "" {
		return nil, errors.New("opnsense-probe: -target required")
	}
	return pf, nil
}

// runOPNsenseProbe is the operational tool for ad hoc dialing of the
// mwan-opnsense daemon over its OOB virtio-serial Unix socket.
func runOPNsenseProbe(args []string) error {
	pf, err := parseProbeFlags(args)
	if err != nil {
		return err
	}

	cli, err := opnsense.Dial(pf.target)
	if err != nil {
		return probeLogWrap(context.Background(), "dial "+pf.target, err)
	}
	defer func() { _ = cli.Close() }()
	rpc := cli.RPC()

	ctx, cancel := context.WithTimeout(context.Background(), pf.timeout)
	defer cancel()
	return dispatchProbeOp(ctx, cli, rpc, pf)
}

// dispatchProbeOp routes the parsed flags to the per-op probe helper.
func dispatchProbeOp(ctx context.Context, cli *opnsense.Client, rpc *opnsense.RPC, pf *probeFlags) error {
	switch probeOp(pf.op) {
	case opVersion:
		return probeVersion(ctx, rpc)
	case opReadConfig:
		return probeReadConfig(ctx, rpc)
	case opWriteConfig:
		return probeWriteConfig(ctx, rpc, pf.configXML, pf.label)
	case opBackupConfig:
		return probeBackupConfig(ctx, rpc, pf.label)
	case opXPathGet:
		return probeXPathGet(ctx, rpc, pf.xpath)
	case opXPathSet:
		return probeXPathSet(ctx, rpc, pf.xpath, pf.xpathValue)
	case opXPathDelete:
		return probeXPathDelete(ctx, rpc, pf.xpath)
	case opExec:
		return probeExec(ctx, rpc, pf.cmd, pf.cmdArgs, pf.cmdSudo, pf.cmdTimeout)
	case opStripGatewayV6:
		return probeStripGatewayV6(ctx, rpc)
	case opInjectGatewayV6:
		return probeInjectGatewayV6(ctx, rpc, pf.gatewayName)
	case opDeployStatus:
		return probeDeployStatus(ctx, rpc)
	case opRevert:
		return probeRevert(ctx, rpc)
	case opSelftest:
		return probeSelftest(ctx, rpc, pf.selftestSize)
	case opTransferUp:
		return probeTransferUp(ctx, cli, pf)
	case opTransferDown:
		return probeTransferDown(ctx, cli, pf)
	case opTransferGC:
		return probeTransferGC(ctx, cli)
	case opDeploy:
		return probeDeploy(ctx, cli, pf.deployBinary)
	default:
		return fmt.Errorf("opnsense-probe: unknown op %q", pf.op)
	}
}

func probeVersion(ctx context.Context, rpc *opnsense.RPC) error {
	resp, err := rpc.Version(ctx, &mwanv1.VersionRequest{})
	if err != nil {
		return probeLogWrap(ctx, "version", err)
	}
	fmt.Printf("version=%s commit=%s dirty=%t binhash=%s\n",
		resp.GetVersion(), resp.GetBuildCommit(), resp.GetBuildDirty(), resp.GetBuildBinhash())
	return nil
}

func probeReadConfig(ctx context.Context, rpc *opnsense.RPC) error {
	resp, err := rpc.ReadConfigXML(ctx)
	if err != nil {
		return probeLogWrap(ctx, "read-config", err)
	}
	fmt.Printf("config_bytes=%d sha256=%s\n", resp.SizeBytes, resp.Sha256)
	return nil
}

func probeWriteConfig(ctx context.Context, rpc *opnsense.RPC, path, label string) error {
	if path == "" {
		return errors.New("opnsense-probe: -config-xml required for op=write-config")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		slog.ErrorContext(ctx, "probe: read xml", "err", err, "path", path)
		return fmt.Errorf("read %s: %w", path, err)
	}
	resp, err := rpc.WriteConfigXML(ctx, content, label)
	if err != nil {
		return probeLogWrap(ctx, "write-config", err)
	}
	fmt.Printf("backup_path=%s bytes_written=%d\n", resp.BackupPath, resp.BytesWritten)
	return nil
}

func probeBackupConfig(ctx context.Context, rpc *opnsense.RPC, label string) error {
	resp, err := rpc.BackupConfigXML(ctx, &mwanv1.BackupConfigXMLRequest{Label: label})
	if err != nil {
		return probeLogWrap(ctx, "backup-config", err)
	}
	fmt.Printf("backup_path=%s size_bytes=%d\n", resp.GetBackupPath(), resp.GetSizeBytes())
	return nil
}

func probeXPathGet(ctx context.Context, rpc *opnsense.RPC, expr string) error {
	matches, err := rpc.XPathGet(ctx, &mwanv1.XPathGetRequest{Expression: expr})
	if err != nil {
		return probeLogWrap(ctx, "xpath-get", err)
	}
	for _, m := range matches {
		fmt.Println(m)
	}
	return nil
}

func probeXPathSet(ctx context.Context, rpc *opnsense.RPC, expr, value string) error {
	resp, err := rpc.XPathSet(ctx, &mwanv1.XPathSetRequest{Expression: expr, NewValue: value})
	if err != nil {
		return probeLogWrap(ctx, "xpath-set", err)
	}
	fmt.Printf("backup_path=%s changed_count=%d\n", resp.GetBackupPath(), resp.GetChangedCount())
	return nil
}

func probeXPathDelete(ctx context.Context, rpc *opnsense.RPC, expr string) error {
	resp, err := rpc.XPathDelete(ctx, &mwanv1.XPathDeleteRequest{Expression: expr})
	if err != nil {
		return probeLogWrap(ctx, "xpath-delete", err)
	}
	fmt.Printf("backup_path=%s deleted_count=%d\n", resp.GetBackupPath(), resp.GetDeletedCount())
	return nil
}

func probeExec(ctx context.Context, rpc *opnsense.RPC, cmd, args string, sudo bool, timeout time.Duration) error {
	if cmd == "" {
		return errors.New("opnsense-probe: -cmd required for op=exec")
	}
	var argv []string
	if args != "" {
		argv = strings.Split(args, ",")
	}
	timeoutSec := min(int32(timeout.Seconds()), probeMaxCommandTimeoutSec)
	res, err := rpc.Exec(ctx, cmd, argv, sudo, timeoutSec, nil)
	if err != nil {
		return probeLogWrap(ctx, "exec", err)
	}
	fmt.Printf("exit_code=%d duration_ms=%d timed_out=%t\n",
		res.ExitCode, res.DurationMs, res.TimedOut)
	if len(res.Stdout) > 0 {
		fmt.Printf("---stdout---\n%s\n", res.Stdout)
	}
	if len(res.Stderr) > 0 {
		fmt.Printf("---stderr---\n%s\n", res.Stderr)
	}
	return nil
}

func probeStripGatewayV6(ctx context.Context, rpc *opnsense.RPC) error {
	resp, err := rpc.StripGatewayV6(ctx, &mwanv1.StripGatewayV6Request{})
	if err != nil {
		return probeLogWrap(ctx, "strip-gatewayv6", err)
	}
	fmt.Printf("backup_path=%s changed=%t\n", resp.GetBackupPath(), resp.GetChanged())
	return nil
}

func probeInjectGatewayV6(ctx context.Context, rpc *opnsense.RPC, name string) error {
	if name == "" {
		return errors.New("opnsense-probe: -gateway-name required for op=inject-gatewayv6")
	}
	resp, err := rpc.InjectGatewayV6(ctx, &mwanv1.InjectGatewayV6Request{GatewayName: name})
	if err != nil {
		return probeLogWrap(ctx, "inject-gatewayv6", err)
	}
	fmt.Printf("backup_path=%s changed=%t\n", resp.GetBackupPath(), resp.GetChanged())
	return nil
}

func probeDeployStatus(ctx context.Context, rpc *opnsense.RPC) error {
	resp, err := rpc.DeployStatus(ctx, &mwanv1.DeployStatusRequest{})
	if err != nil {
		return probeLogWrap(ctx, "deploy-status", err)
	}
	fmt.Printf("active=%s previous=%s health=%s deployed_at=%d\n",
		resp.GetActiveSha256(), resp.GetPreviousSha256(), resp.GetHealth(), resp.GetDeployedAt())
	return nil
}

func probeRevert(ctx context.Context, rpc *opnsense.RPC) error {
	resp, err := rpc.Revert(ctx, &mwanv1.RevertRequest{})
	if err != nil {
		return probeLogWrap(ctx, "revert", err)
	}
	fmt.Printf("reverted_to=%s re_exec_started=%t\n", resp.GetRevertedToSha256(), resp.GetReExecStarted())
	return nil
}

func probeSelftest(ctx context.Context, rpc *opnsense.RPC, size int) error {
	if size <= 0 {
		size = 1024
	}
	payload := make([]byte, size)
	if _, err := cryptorand.Read(payload); err != nil {
		return probeLogWrap(ctx, "rand", err)
	}
	res, err := rpc.Exec(ctx, "/usr/bin/wc", []string{"-c"}, false, 30, payload)
	if err != nil {
		return probeLogWrap(ctx, "selftest exec", err)
	}
	fmt.Printf("selftest size=%d exit=%d stdout_bytes=%d stderr_bytes=%d duration_ms=%d\n",
		size, res.ExitCode, len(res.Stdout), len(res.Stderr), res.DurationMs)
	return nil
}

func probeTransferUp(ctx context.Context, cli *opnsense.Client, pf *probeFlags) error {
	if pf.transferPath == "" {
		return errors.New("opnsense-probe: -path required for op=transfer-up")
	}
	payload, size, source, err := loadTransferUpPayload(ctx, pf)
	if err != nil {
		return err
	}
	if pf.transferResume {
		return probeTransferUpResume(ctx, cli, pf, payload, size, source)
	}
	return probeTransferUpFresh(ctx, cli, pf, payload, size, source)
}

// loadTransferUpPayload resolves the bytes the probe will upload.
// When -source is set, the file is read in full; otherwise the probe
// falls back to the historical random-payload behavior driven by
// -size. The returned source string is empty when no -source was given.
func loadTransferUpPayload(ctx context.Context, pf *probeFlags) ([]byte, int64, string, error) {
	if pf.transferSource != "" {
		content, readErr := os.ReadFile(pf.transferSource)
		if readErr != nil {
			slog.ErrorContext(ctx, "probe: read source", "err", readErr, "source", pf.transferSource)
			return nil, 0, "", fmt.Errorf("read %s: %w", pf.transferSource, readErr)
		}
		return content, int64(len(content)), pf.transferSource, nil
	}
	if pf.transferResume {
		return nil, 0, "", errors.New("opnsense-probe: -resume requires -source")
	}
	if pf.transferSize <= 0 {
		return nil, 0, "", errors.New("opnsense-probe: -size or -source required for op=transfer-up")
	}
	payload := make([]byte, pf.transferSize)
	if _, err := cryptorand.Read(payload); err != nil {
		slog.ErrorContext(ctx, "probe: rand", "err", err)
		return nil, 0, "", probeLogWrap(ctx, "rand", err)
	}
	return payload, pf.transferSize, "", nil
}

// probeTransferUpFresh runs the first-attempt upload path. When a
// source path is known and the state file would be useful, the probe
// persists checkpoints so a later -resume invocation can continue.
func probeTransferUpFresh(ctx context.Context, cli *opnsense.Client, pf *probeFlags, payload []byte, size int64, source string) error {
	stream, err := cli.TransferClient().Upload(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "probe: transfer up open", "err", err)
		return probeLogWrap(ctx, "transfer-up: open", err)
	}
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Header{Header: &mwanv1.TransferHeader{
			Path:       pf.transferPath,
			Direction:  mwanv1.TransferDirection_TRANSFER_DIRECTION_WRITE,
			FinishStep: mwanv1.FinishStep_FINISH_STEP_REPLACE,
			Label:      pf.label,
			TotalSize:  size,
		}},
	}); sendErr != nil {
		return probeLogWrap(ctx, "transfer-up: send header", sendErr)
	}
	ackMsg, recvErr := stream.Recv()
	if recvErr != nil {
		return probeLogWrap(ctx, "transfer-up: recv ack", recvErr)
	}
	ack := ackMsg.GetAck()
	if ack == nil {
		return errors.New("transfer-up: first response was not an ack")
	}
	state := &probeTransferState{
		TransferID:      ack.GetTransferId(),
		SourcePath:      source,
		RemotePath:      pf.transferPath,
		Label:           pf.label,
		TotalSize:       size,
		CommittedOffset: 0,
		HashState:       nil,
	}
	hasher := sha256.New()
	if streamErr := uploadDataChunks(ctx, stream, payload, 0, size, hasher, state, source != ""); streamErr != nil {
		return streamErr
	}
	return finalizeTransferUp(ctx, stream, payload, source)
}

// probeTransferUpResume reads the persisted state for source and
// continues the upload from the committed offset. It errors out
// cleanly when no state exists, when the remote daemon refuses the
// resume (ResumeAccepted=false), or when -transfer-id is given but
// does not match.
func probeTransferUpResume(ctx context.Context, cli *opnsense.Client, pf *probeFlags, payload []byte, size int64, source string) error {
	state, loadErr := lookupResumeState(ctx, source, pf.transferID)
	if loadErr != nil {
		return loadErr
	}
	if state.RemotePath != pf.transferPath {
		return fmt.Errorf("opnsense-probe: -resume: state remote_path %q != -path %q",
			state.RemotePath, pf.transferPath)
	}
	if state.TotalSize != size {
		return fmt.Errorf("opnsense-probe: -resume: state total_size %d != source size %d",
			state.TotalSize, size)
	}
	hasher, hashErr := unmarshalHashState(ctx, state.HashState)
	if hashErr != nil {
		return hashErr
	}

	stream, err := cli.TransferClient().Upload(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "probe: transfer up open", "err", err)
		return probeLogWrap(ctx, "transfer-up: open", err)
	}
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Header{Header: &mwanv1.TransferHeader{
			Path:              pf.transferPath,
			Direction:         mwanv1.TransferDirection_TRANSFER_DIRECTION_WRITE,
			FinishStep:        mwanv1.FinishStep_FINISH_STEP_REPLACE,
			Label:             state.Label,
			TotalSize:         size,
			ResumeTransferId:  state.TransferID,
			ResumeFromOffset:  state.CommittedOffset,
			ResumeSha256State: state.HashState,
		}},
	}); sendErr != nil {
		return probeLogWrap(ctx, "transfer-up: send header", sendErr)
	}
	ackMsg, recvErr := stream.Recv()
	if recvErr != nil {
		return probeLogWrap(ctx, "transfer-up: recv ack", recvErr)
	}
	ack := ackMsg.GetAck()
	if ack == nil {
		return errors.New("transfer-up: first response was not an ack")
	}
	if !ack.GetResumeAccepted() {
		return fmt.Errorf("opnsense-probe: -resume: daemon refused; delete %s and retry without -resume",
			stateFilePathOrEmpty(ctx, state.TransferID))
	}
	if ack.GetCommittedOffset() != state.CommittedOffset {
		return fmt.Errorf("opnsense-probe: -resume: ack offset %d != client offset %d",
			ack.GetCommittedOffset(), state.CommittedOffset)
	}

	if streamErr := uploadDataChunks(ctx, stream, payload, state.CommittedOffset, size, hasher, state, true); streamErr != nil {
		return streamErr
	}
	return finalizeTransferUp(ctx, stream, payload, source)
}

// lookupResumeState resolves the state file for the resume operation
// by transfer id (when provided) or by source path lookup.
func lookupResumeState(ctx context.Context, source, transferID string) (*probeTransferState, error) {
	if transferID != "" {
		path, pathErr := probeStatePath(ctx, transferID)
		if pathErr != nil {
			return nil, pathErr
		}
		state, loadErr := loadProbeState(ctx, path)
		if loadErr != nil {
			return nil, loadErr
		}
		if source != "" && state.SourcePath != source {
			return nil, fmt.Errorf("opnsense-probe: -resume: state source %q != -source %q",
				state.SourcePath, source)
		}
		return state, nil
	}
	path, findErr := findProbeStateBySource(ctx, source)
	if findErr != nil {
		return nil, findErr
	}
	if path == "" {
		return nil, fmt.Errorf("opnsense-probe: -resume: no state file for source %q; run once without -resume first", source)
	}
	return loadProbeState(ctx, path)
}

// stateFilePathOrEmpty returns the on-disk path for transferID, or an
// empty string when the path lookup fails. Used in error messages only.
func stateFilePathOrEmpty(ctx context.Context, transferID string) string {
	path, err := probeStatePath(ctx, transferID)
	if err != nil {
		return ""
	}
	return path
}

// uploadDataChunks sends data frames from start..size, hashes them,
// and persists a checkpoint after every chunk so a future -resume can
// pick up. When persistState is false, no state is written.
func uploadDataChunks(ctx context.Context, stream mwanv1.TransferService_UploadClient, payload []byte, start, size int64, hasher hash.Hash, state *probeTransferState, persistState bool) error {
	offset := start
	for offset < size {
		end := min(offset+probeDefaultUploadChunk, size)
		chunk := payload[offset:end]
		if _, hashErr := hasher.Write(chunk); hashErr != nil {
			return probeLogWrap(ctx, "transfer-up: hash", hashErr)
		}
		if sendErr := stream.Send(&mwanv1.UploadRequest{
			Body: &mwanv1.UploadRequest_Data{Data: &mwanv1.TransferDataChunk{
				Offset: offset,
				Data:   chunk,
			}},
		}); sendErr != nil {
			return probeLogWrap(ctx, "transfer-up: send data", sendErr)
		}
		offset = end
		if persistState {
			snap, snapErr := marshalHashState(ctx, hasher)
			if snapErr != nil {
				return snapErr
			}
			state.CommittedOffset = offset
			state.HashState = snap
			if saveErr := saveProbeState(ctx, state); saveErr != nil {
				return saveErr
			}
		}
	}
	return nil
}

// finalizeTransferUp sends the Final, waits for the Terminal, and
// removes the persisted state on success. payload is hashed in full
// to compute the canonical SHA so the daemon can verify it.
func finalizeTransferUp(ctx context.Context, stream mwanv1.TransferService_UploadClient, payload []byte, source string) error {
	sum := sha256.Sum256(payload)
	finalHex := hex.EncodeToString(sum[:])
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Final{Final: &mwanv1.TransferFinal{Sha256Hex: finalHex}},
	}); sendErr != nil {
		return probeLogWrap(ctx, "transfer-up: send final", sendErr)
	}
	if closeErr := stream.CloseSend(); closeErr != nil {
		return probeLogWrap(ctx, "transfer-up: close send", closeErr)
	}
	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			return errors.New("transfer up: stream ended before terminal")
		}
		if recvErr != nil {
			return probeLogWrap(ctx, "transfer-up: recv", recvErr)
		}
		if term := msg.GetTerminal(); term != nil {
			fmt.Printf("transfer up: bytes=%d sha256=%s backup_path=%s\n",
				term.GetTotalBytes(), term.GetSha256Hex(), term.GetBackupPath())
			if source != "" {
				if delErr := deleteProbeStateForSource(ctx, source); delErr != nil {
					slog.WarnContext(ctx, "probe: delete state after success", "err", delErr, "source", source)
				}
			}
			return nil
		}
	}
}

// deleteProbeStateForSource finds and removes the state file for
// source, if any. Used after a successful upload to keep the state
// directory tidy.
func deleteProbeStateForSource(ctx context.Context, source string) error {
	path, findErr := findProbeStateBySource(ctx, source)
	if findErr != nil {
		return findErr
	}
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.ErrorContext(ctx, "probe: remove state", "err", err, "path", path)
		return fmt.Errorf("probe state: remove %s: %w", path, err)
	}
	return nil
}

func probeTransferDown(ctx context.Context, cli *opnsense.Client, pf *probeFlags) error {
	if pf.transferPath == "" {
		return errors.New("opnsense-probe: -path required for op=transfer-down")
	}
	if pf.transferResume {
		return errors.New("opnsense-probe: -resume is not supported for op=transfer-down (daemon serves reads without state)")
	}
	remotePath := pf.transferPath
	stream, err := cli.TransferClient().Upload(ctx)
	if err != nil {
		return probeLogWrap(ctx, "transfer-down: open", err)
	}
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Header{Header: &mwanv1.TransferHeader{
			Path:      remotePath,
			Direction: mwanv1.TransferDirection_TRANSFER_DIRECTION_READ,
		}},
	}); sendErr != nil {
		return probeLogWrap(ctx, "transfer-down: send header", sendErr)
	}
	if closeErr := stream.CloseSend(); closeErr != nil {
		return probeLogWrap(ctx, "transfer-down: close send", closeErr)
	}
	hasher := sha256.New()
	total := int64(0)
	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return probeLogWrap(ctx, "transfer-down: recv", recvErr)
		}
		if data := msg.GetData(); data != nil {
			if _, hashErr := hasher.Write(data.GetData()); hashErr != nil {
				return probeLogWrap(ctx, "transfer-down: hash", hashErr)
			}
			total += int64(len(data.GetData()))
		}
		if term := msg.GetTerminal(); term != nil {
			fmt.Printf("transfer down: bytes=%d sha256=%s server_sha256=%s\n",
				total, hex.EncodeToString(hasher.Sum(nil)), term.GetSha256Hex())
			return nil
		}
	}
	return nil
}

// probeTransferGC exercises the Cancel surface to confirm the gRPC
// channel is reachable. The daemon runs its sweep on startup, so there
// is nothing for the probe to drive from the client side.
func probeTransferGC(ctx context.Context, cli *opnsense.Client) error {
	if _, err := cli.TransferClient().Cancel(ctx, &mwanv1.CancelRequest{TransferId: ""}); err != nil {
		slog.WarnContext(ctx, "transfer-gc: cancel surface", "err", err)
	}
	fmt.Println("transfer-gc: server runs GC on startup; nothing to do from probe")
	return nil
}

func probeDeploy(ctx context.Context, cli *opnsense.Client, binaryPath string) error {
	if binaryPath == "" {
		return errors.New("opnsense-probe: -binary required for op=deploy")
	}
	content, err := os.ReadFile(binaryPath)
	if err != nil {
		slog.ErrorContext(ctx, "probe: read binary", "err", err, "path", binaryPath)
		return fmt.Errorf("read %s: %w", binaryPath, err)
	}
	stagedSHA, err := deployStage(ctx, cli, binaryPath, content)
	if err != nil {
		return err
	}
	commitResp, err := cli.OpnsenseClient().CommitDeploy(ctx, &mwanv1.CommitDeployRequest{
		StagedSha256: stagedSHA,
	})
	if err != nil {
		slog.ErrorContext(ctx, "probe: commit", "err", err)
		return probeLogWrap(ctx, "deploy: commit", err)
	}
	fmt.Printf("deploy: committed active=%s previous=%s re_exec=%t\n",
		commitResp.GetActiveSha256(), commitResp.GetPreviousPath(), commitResp.GetReExecStarted())
	return nil
}

func deployStage(ctx context.Context, cli *opnsense.Client, binaryPath string, content []byte) (string, error) {
	stream, err := cli.TransferClient().Upload(ctx)
	if err != nil {
		return probeLogWrapStr(ctx, "deploy: open upload", err)
	}
	target := filepath.Join("/usr/local/sbin", "mwan-opnsense")
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Header{Header: &mwanv1.TransferHeader{
			Path:       target,
			Direction:  mwanv1.TransferDirection_TRANSFER_DIRECTION_WRITE,
			FinishStep: mwanv1.FinishStep_FINISH_STEP_STAGE,
			TotalSize:  int64(len(content)),
		}},
	}); sendErr != nil {
		slog.ErrorContext(ctx, "probe: deploy send header", "err", sendErr, "binary", binaryPath)
		return probeLogWrapStr(ctx, "deploy: send header", sendErr)
	}
	if _, recvErr := stream.Recv(); recvErr != nil {
		return probeLogWrapStr(ctx, "deploy: recv ack", recvErr)
	}
	offset := int64(0)
	for offset < int64(len(content)) {
		end := min(offset+probeDefaultUploadChunk, int64(len(content)))
		if sendErr := stream.Send(&mwanv1.UploadRequest{
			Body: &mwanv1.UploadRequest_Data{Data: &mwanv1.TransferDataChunk{
				Offset: offset, Data: content[offset:end],
			}},
		}); sendErr != nil {
			return probeLogWrapStr(ctx, "deploy: send data", sendErr)
		}
		offset = end
	}
	sum := sha256.Sum256(content)
	finalHex := hex.EncodeToString(sum[:])
	if sendErr := stream.Send(&mwanv1.UploadRequest{
		Body: &mwanv1.UploadRequest_Final{Final: &mwanv1.TransferFinal{Sha256Hex: finalHex}},
	}); sendErr != nil {
		return probeLogWrapStr(ctx, "deploy: send final", sendErr)
	}
	if closeErr := stream.CloseSend(); closeErr != nil {
		return probeLogWrapStr(ctx, "deploy: close send", closeErr)
	}
	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			return "", errors.New("deploy: never received terminal")
		}
		if recvErr != nil {
			return probeLogWrapStr(ctx, "deploy: recv", recvErr)
		}
		if term := msg.GetTerminal(); term != nil {
			stagedSHA := term.GetSha256Hex()
			fmt.Printf("deploy: staged bytes=%d sha256=%s staged_path=%s\n",
				term.GetTotalBytes(), stagedSHA, term.GetStagedPath())
			return stagedSHA, nil
		}
	}
}
