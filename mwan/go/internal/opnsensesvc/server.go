package opnsensesvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/version"
)

var errDeployChunkProtocol = errors.New("deploy chunk protocol error")

// clampToInt32 saturates n to the int32 range. Used for proto fields
// that carry small counts but originate from int-typed callees.
func clampToInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < math.MinInt32 {
		return math.MinInt32
	}
	return int32(n)
}

type deployChunkWriter struct {
	destination io.Writer
	hash        io.Writer
	header      *mwanv1.ChunkHeader
	bytesIn     int64
	headerSeen  bool
	done        bool
	checksumOK  bool
	computedHex string
	trailerHex  string
	hashSum     func() []byte
}

func newDeployChunkWriter(destination io.Writer) *deployChunkWriter {
	hash := sha256.New()
	return &deployChunkWriter{
		destination: destination,
		hash:        hash,
		header:      nil,
		bytesIn:     0,
		headerSeen:  false,
		done:        false,
		checksumOK:  false,
		computedHex: "",
		trailerHex:  "",
		hashSum: func() []byte {
			return hash.Sum(nil)
		},
	}
}

func (w *deployChunkWriter) Write(chunk *mwanv1.Chunk) error {
	if chunk == nil {
		return deployChunkProtocolError("nil chunk")
	}
	if w.done {
		return deployChunkProtocolError("chunk after trailer")
	}
	switch body := chunk.GetBody().(type) {
	case *mwanv1.Chunk_Header:
		if w.headerSeen {
			return deployChunkProtocolError("duplicate header")
		}
		w.headerSeen = true
		w.header = body.Header
		return nil
	case *mwanv1.Chunk_Data:
		if !w.headerSeen {
			return deployChunkProtocolError("data before header")
		}
		return w.writeData(body.Data)
	case *mwanv1.Chunk_Trailer:
		if !w.headerSeen {
			return deployChunkProtocolError("trailer before header")
		}
		w.done = true
		w.computedHex = hex.EncodeToString(w.hashSum())
		w.trailerHex = body.Trailer.GetSha256Hex()
		w.checksumOK = strings.EqualFold(w.trailerHex, w.computedHex)
		return nil
	default:
		return deployChunkProtocolError("unknown chunk body")
	}
}

func (w *deployChunkWriter) writeData(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if _, err := w.destination.Write(data); err != nil {
		slog.Error("Deploy: write chunk data failed", "err", err, "bytes", len(data))
		return fmt.Errorf("write deploy chunk data: %w", err)
	}
	if _, err := w.hash.Write(data); err != nil {
		slog.Error("Deploy: hash chunk data failed", "err", err, "bytes", len(data))
		return fmt.Errorf("hash deploy chunk data: %w", err)
	}
	w.bytesIn += int64(len(data))
	return nil
}

func deployChunkProtocolError(reason string) error {
	wrapped := fmt.Errorf("%w: %s", errDeployChunkProtocol, reason)
	slog.Warn("Deploy: chunk protocol violation", "err", wrapped, "reason", reason)
	return wrapped
}

// Server implements the MWANOPNsense RPC handlers. It is no longer a
// gRPC server; the MWN1 Dispatcher dispatches each method directly to
// the matching method on this struct.
//
// All write operations on /conf/config.xml take a snapshot first via
// backupConfig and return the backup path, so a client can immediately
// revert by calling WriteConfigXML with the snapshot bytes.
type Server struct {
	configPath string
	backupDir  string
	log        *slog.Logger
	clock      Clock
	deploy     *DeployManager
	mu         sync.Mutex // serializes mutating ops on config.xml
}

// NewServer constructs a Server. configPath defaults to ConfigPath
// when empty; backupDir defaults to BackupDir when empty. The
// DeployManager uses package defaults; override with NewServerWithDeploy
// for tests.
func NewServer(log *slog.Logger, configPath, backupDir string) *Server {
	cfg := DeployConfig{
		BinaryDir:      "",
		StatePath:      "",
		PendingPath:    "",
		ReExecGrace:    0,
		ReExecFn:       nil,
		WriteStateFile: nil,
		NowFn:          nil,
	}
	return NewServerWithDeploy(log, configPath, backupDir, NewDeployManager(log, cfg))
}

// NewServerWithDeploy is the explicit form used by tests to inject
// a tmpdir-rooted DeployManager.
func NewServerWithDeploy(log *slog.Logger, configPath, backupDir string, deploy *DeployManager) *Server {
	if configPath == "" {
		configPath = ConfigPath
	}
	if backupDir == "" {
		backupDir = BackupDir
	}
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		configPath: configPath,
		backupDir:  backupDir,
		log:        log,
		clock:      realClock{},
		deploy:     deploy,
		mu:         sync.Mutex{},
	}
}

// Version returns the build metadata of the running binary.
func (s *Server) Version(_ context.Context, _ *mwanv1.VersionRequest) (*mwanv1.VersionResponse, error) {
	return &mwanv1.VersionResponse{
		Version:      version.BuildVersionString(),
		BuildCommit:  version.GitCommit(),
		BuildDirty:   version.GitDirty() == "dirty",
		BuildBinhash: version.BinaryHash(),
	}, nil
}

// Exec runs an arbitrary command. Access is constrained by the
// root-only Proxmox-side unix socket for the virtio-serial channel.
// The command is forwarded to runExec for execution and output capture.
func (s *Server) Exec(ctx context.Context, req *mwanv1.ExecRequest) (*mwanv1.ExecResponse, error) {
	s.log.InfoContext(ctx, "opnsensesvc: Exec",
		"exec_command", req.GetCommand(),
		"args_len", len(req.GetArgs()),
		"sudo", req.GetSudo(),
		"timeout_seconds", req.GetTimeoutSeconds())

	res, err := runExec(ctx, ExecArgs{
		Command:        req.GetCommand(),
		Args:           req.GetArgs(),
		Sudo:           req.GetSudo(),
		TimeoutSeconds: req.GetTimeoutSeconds(),
		StdinBytes:     req.GetStdinBytes(),
		Clock:          s.clock,
		Log:            s.log,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: Exec failed",
			"err", err, "exec_command", req.GetCommand())
		return nil, fmt.Errorf("exec: %w", err)
	}
	return &mwanv1.ExecResponse{
		Stdout:          res.Stdout,
		Stderr:          res.Stderr,
		ExitCode:        res.ExitCode,
		DurationMs:      res.DurationMS,
		StdoutTruncated: res.StdoutTruncated,
		StderrTruncated: res.StderrTruncated,
		TimedOut:        res.TimedOut,
	}, nil
}

// ReadConfigXML returns the full /conf/config.xml content.
func (s *Server) ReadConfigXML(
	ctx context.Context,
	_ *mwanv1.ReadConfigXMLRequest,
) (*mwanv1.ReadConfigXMLResponse, error) {
	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: ReadConfigXML failed",
			"err", err, "path", s.configPath)
		return nil, fmt.Errorf("read: %w", err)
	}
	sum := sha256.Sum256(content)
	encodedSum := hex.EncodeToString(sum[:])
	s.log.InfoContext(ctx, "opnsensesvc: ReadConfigXML",
		"size_bytes", len(content), "sha256", encodedSum)
	return &mwanv1.ReadConfigXMLResponse{
		Content:   content,
		SizeBytes: int64(len(content)),
		Sha256:    encodedSum,
	}, nil
}

// WriteConfigXML snapshots the current config first, then writes
// the new content atomically.
func (s *Server) WriteConfigXML(
	ctx context.Context,
	req *mwanv1.WriteConfigXMLRequest,
) (*mwanv1.WriteConfigXMLResponse, error) {
	if len(req.GetContent()) == 0 {
		return nil, errors.New("content empty")
	}
	activeClock := clockOrReal(s.clock)
	startedAt := activeClock.Now()
	s.log.InfoContext(ctx, "opnsensesvc: WriteConfigXML start",
		"bytes_in", len(req.GetContent()),
		"label", req.GetLabel())
	lockStartedAt := activeClock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.InfoContext(ctx, "opnsensesvc: WriteConfigXML lock acquired",
		"bytes_in", len(req.GetContent()),
		"lock_wait_ms", activeClock.Now().Sub(lockStartedAt).Milliseconds())

	backupStartedAt := activeClock.Now()
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, req.GetLabel())
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: WriteConfigXML backup failed",
			"err", err,
			"label", req.GetLabel(),
			"bytes_in", len(req.GetContent()),
			"backup_duration_ms", activeClock.Now().Sub(backupStartedAt).Milliseconds(),
			"total_duration_ms", activeClock.Now().Sub(startedAt).Milliseconds())
		return nil, fmt.Errorf("backup: %w", err)
	}
	s.log.InfoContext(ctx, "opnsensesvc: WriteConfigXML backup complete",
		"backup_path", backupPath,
		"backup_duration_ms", activeClock.Now().Sub(backupStartedAt).Milliseconds())
	writeStartedAt := activeClock.Now()
	if writeErr := writeConfigWithLog(ctx, s.log, s.configPath, req.GetContent()); writeErr != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: WriteConfigXML write failed",
			"err", writeErr,
			"backup_path", backupPath,
			"bytes_in", len(req.GetContent()),
			"write_duration_ms", activeClock.Now().Sub(writeStartedAt).Milliseconds(),
			"total_duration_ms", activeClock.Now().Sub(startedAt).Milliseconds())
		return nil, fmt.Errorf("write: %w", writeErr)
	}
	s.log.InfoContext(ctx, "opnsensesvc: WriteConfigXML",
		"backup_path", backupPath,
		"bytes_written", len(req.GetContent()),
		"write_duration_ms", activeClock.Now().Sub(writeStartedAt).Milliseconds(),
		"total_duration_ms", activeClock.Now().Sub(startedAt).Milliseconds())
	return &mwanv1.WriteConfigXMLResponse{
		BackupPath:   backupPath,
		BytesWritten: int64(len(req.GetContent())),
	}, nil
}

// BackupConfigXML takes an explicit snapshot.
func (s *Server) BackupConfigXML(
	ctx context.Context,
	req *mwanv1.BackupConfigXMLRequest,
) (*mwanv1.BackupConfigXMLResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, req.GetLabel())
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: BackupConfigXML failed", "err", err)
		return nil, fmt.Errorf("backup: %w", err)
	}
	content, err := readConfigWithLog(ctx, s.log, backupPath)
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: BackupConfigXML stat failed", "err", err)
		return nil, fmt.Errorf("stat: %w", err)
	}
	s.log.InfoContext(ctx, "opnsensesvc: BackupConfigXML",
		"backup_path", backupPath, "size_bytes", len(content))
	return &mwanv1.BackupConfigXMLResponse{
		BackupPath: backupPath,
		SizeBytes:  int64(len(content)),
	}, nil
}

// XPathGet runs a read-only XPath query.
func (s *Server) XPathGet(
	ctx context.Context,
	req *mwanv1.XPathGetRequest,
) (*mwanv1.XPathGetResponse, error) {
	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathGet read failed", "err", err)
		return nil, fmt.Errorf("read: %w", err)
	}
	matches, err := xpathGetWithLog(ctx, s.log, content, req.GetExpression())
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathGet query failed",
			"err", err, "expression", req.GetExpression())
		return nil, fmt.Errorf("xpath: %w", err)
	}
	s.log.InfoContext(ctx, "opnsensesvc: XPathGet",
		"expression", req.GetExpression(), "matches", len(matches))
	return &mwanv1.XPathGetResponse{Matches: matches}, nil
}

// XPathSet snapshots first, applies the change, writes atomically.
func (s *Server) XPathSet(
	ctx context.Context,
	req *mwanv1.XPathSetRequest,
) (*mwanv1.XPathSetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathSet read failed", "err", err)
		return nil, fmt.Errorf("read: %w", err)
	}
	updated, n, err := xpathSetWithLog(ctx, s.log,
		content, req.GetExpression(), req.GetNewValue())
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathSet apply failed",
			"err", err, "expression", req.GetExpression())
		return nil, fmt.Errorf("xpath: %w", err)
	}
	if n == 0 {
		return &mwanv1.XPathSetResponse{ChangedCount: 0}, nil
	}
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, "xpath-set")
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathSet backup failed", "err", err)
		return nil, fmt.Errorf("backup: %w", err)
	}
	if writeErr := writeConfigWithLog(ctx, s.log, s.configPath, updated); writeErr != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathSet write failed",
			"err", writeErr, "backup_path", backupPath)
		return nil, fmt.Errorf("write: %w", writeErr)
	}
	s.log.InfoContext(ctx, "opnsensesvc: XPathSet",
		"expression", req.GetExpression(),
		"changed_count", n, "backup_path", backupPath)
	return &mwanv1.XPathSetResponse{
		BackupPath:   backupPath,
		ChangedCount: clampToInt32(n),
	}, nil
}

// XPathDelete snapshots first, deletes matching nodes, writes
// atomically.
func (s *Server) XPathDelete(
	ctx context.Context,
	req *mwanv1.XPathDeleteRequest,
) (*mwanv1.XPathDeleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathDelete read failed", "err", err)
		return nil, fmt.Errorf("read: %w", err)
	}
	updated, n, err := xpathDeleteWithLog(ctx, s.log, content, req.GetExpression())
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathDelete apply failed",
			"err", err, "expression", req.GetExpression())
		return nil, fmt.Errorf("xpath: %w", err)
	}
	if n == 0 {
		return &mwanv1.XPathDeleteResponse{DeletedCount: 0}, nil
	}
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, "xpath-delete")
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathDelete backup failed", "err", err)
		return nil, fmt.Errorf("backup: %w", err)
	}
	if writeErr := writeConfigWithLog(ctx, s.log, s.configPath, updated); writeErr != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: XPathDelete write failed",
			"err", writeErr, "backup_path", backupPath)
		return nil, fmt.Errorf("write: %w", writeErr)
	}
	s.log.InfoContext(ctx, "opnsensesvc: XPathDelete",
		"expression", req.GetExpression(),
		"deleted_count", n, "backup_path", backupPath)
	return &mwanv1.XPathDeleteResponse{
		BackupPath:   backupPath,
		DeletedCount: clampToInt32(n),
	}, nil
}

// StripGatewayV6 is the convenience wrapper for the cutover path.
func (s *Server) StripGatewayV6(
	ctx context.Context,
	_ *mwanv1.StripGatewayV6Request,
) (*mwanv1.StripGatewayV6Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: StripGatewayV6 read failed", "err", err)
		return nil, fmt.Errorf("read: %w", err)
	}
	updated, changed, err := stripGatewayV6WithLog(ctx, s.log, content)
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: StripGatewayV6 apply failed", "err", err)
		return nil, fmt.Errorf("strip: %w", err)
	}
	if !changed {
		return &mwanv1.StripGatewayV6Response{Changed: false}, nil
	}
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, "strip-gatewayv6")
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: StripGatewayV6 backup failed", "err", err)
		return nil, fmt.Errorf("backup: %w", err)
	}
	if writeErr := writeConfigWithLog(ctx, s.log, s.configPath, updated); writeErr != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: StripGatewayV6 write failed",
			"err", writeErr, "backup_path", backupPath)
		return nil, fmt.Errorf("write: %w", writeErr)
	}
	s.log.InfoContext(ctx, "opnsensesvc: StripGatewayV6", "backup_path", backupPath)
	return &mwanv1.StripGatewayV6Response{
		BackupPath: backupPath,
		Changed:    true,
	}, nil
}

// InjectGatewayV6 is the convenience wrapper for the cutover unfuck
// path.
func (s *Server) InjectGatewayV6(
	ctx context.Context,
	req *mwanv1.InjectGatewayV6Request,
) (*mwanv1.InjectGatewayV6Response, error) {
	if req.GetGatewayName() == "" {
		return nil, errors.New("gateway_name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: InjectGatewayV6 read failed", "err", err)
		return nil, fmt.Errorf("read: %w", err)
	}
	updated, changed, err := injectGatewayV6WithLog(ctx, s.log,
		content, req.GetGatewayName())
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: InjectGatewayV6 apply failed",
			"err", err, "gateway_name", req.GetGatewayName())
		return nil, fmt.Errorf("inject: %w", err)
	}
	if !changed {
		return &mwanv1.InjectGatewayV6Response{Changed: false}, nil
	}
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, "inject-gatewayv6")
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: InjectGatewayV6 backup failed", "err", err)
		return nil, fmt.Errorf("backup: %w", err)
	}
	if writeErr := writeConfigWithLog(ctx, s.log, s.configPath, updated); writeErr != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: InjectGatewayV6 write failed",
			"err", writeErr, "backup_path", backupPath)
		return nil, fmt.Errorf("write: %w", writeErr)
	}
	s.log.InfoContext(ctx, "opnsensesvc: InjectGatewayV6",
		"backup_path", backupPath, "gateway_name", req.GetGatewayName())
	return &mwanv1.InjectGatewayV6Response{
		BackupPath: backupPath,
		Changed:    true,
	}, nil
}

// DeployAttrVersionStr is the ChunkHeader.attrs key carrying the
// human-readable version string for a streaming Deploy upload.
const DeployAttrVersionStr = "version_str"

// Deploy reads a stream of chunked-stream Chunk messages (header,
// data*, trailer) off chunks, writes each chunk's body incrementally to
// a temp file under DeployManager.BinaryDir, verifies the trailer's
// sha256, and hands the temp file off to DeployManager.DeployFromPath.
// The caller (the MWN1 dispatcher) closes chunks once the FlagFinal
// frame arrives. The reply is sent before re-exec so the bridge sees
// the response.
//
// If chunks is closed before a trailer chunk, Deploy returns an error
// without staging the upload. Cancellation via ctx aborts the read loop
// and removes the partial temp file.
func (s *Server) Deploy(ctx context.Context, chunks <-chan *mwanv1.Chunk) (*mwanv1.DeployResponse, error) {
	s.log.InfoContext(ctx, "opnsensesvc: Deploy stream begin")

	if s.deploy == nil {
		return nil, errors.New("Deploy: DeployManager not configured")
	}

	tmpFile, tmpErr := os.CreateTemp(s.deploy.BinaryDir(), "mwan-opnsense.upload-*.bin")
	if tmpErr != nil {
		s.log.ErrorContext(ctx, "Deploy: temp file create failed",
			"err", tmpErr, "dir", s.deploy.BinaryDir())
		return nil, fmt.Errorf("create temp: %w", tmpErr)
	}
	tmpPath := tmpFile.Name()
	cleanedUp := false
	defer func() {
		if cleanedUp {
			return
		}
		if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			s.log.WarnContext(ctx, "Deploy: temp cleanup failed", "err", removeErr, "path", tmpPath)
		}
	}()

	writer := newDeployChunkWriter(tmpFile)
	chunkCount := 0
loop:
	for {
		select {
		case <-ctx.Done():
			_ = tmpFile.Close()
			s.log.WarnContext(ctx, "Deploy: ctx cancelled mid-stream", "err", ctx.Err(), "chunks_seen", chunkCount)
			return nil, fmt.Errorf("Deploy: ctx: %w", ctx.Err())
		case chunk, ok := <-chunks:
			if !ok {
				break loop
			}
			chunkCount++
			if writeErr := writer.Write(chunk); writeErr != nil {
				_ = tmpFile.Close()
				if errors.Is(writeErr, errDeployChunkProtocol) {
					s.log.ErrorContext(ctx, "Deploy: chunk protocol error", "err", writeErr)
					return nil, fmt.Errorf("chunk: %w", writeErr)
				}
				s.log.ErrorContext(ctx, "Deploy: chunk write failed", "err", writeErr)
				return nil, fmt.Errorf("chunk write: %w", writeErr)
			}
		}
	}
	if syncErr := tmpFile.Sync(); syncErr != nil {
		_ = tmpFile.Close()
		s.log.ErrorContext(ctx, "Deploy: temp sync failed", "err", syncErr, "path", tmpPath)
		return nil, fmt.Errorf("sync temp: %w", syncErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		s.log.ErrorContext(ctx, "Deploy: temp close failed", "err", closeErr, "path", tmpPath)
		return nil, fmt.Errorf("close temp: %w", closeErr)
	}
	if !writer.done {
		return nil, errors.New("Deploy: stream ended before trailer")
	}
	if !writer.checksumOK {
		mismatchErr := fmt.Errorf("Deploy: sha256 mismatch: got %s want %s",
			writer.computedHex, writer.trailerHex)
		s.log.ErrorContext(ctx, "Deploy: sha256 mismatch on stream",
			"err", mismatchErr,
			"got", writer.computedHex,
			"want", writer.trailerHex)
		return nil, mismatchErr
	}
	header := writer.header
	versionStr := header.GetAttrs()[DeployAttrVersionStr]
	if versionStr == "" {
		versionStr = header.GetLabel()
	}
	bytesWritten := writer.bytesIn
	s.log.InfoContext(ctx, "opnsensesvc: Deploy stream complete",
		"bytes", bytesWritten, "version_str", versionStr)

	previousPath, stagedSHA256, err := s.deploy.DeployFromPath(ctx, tmpPath, writer.computedHex, versionStr)
	if err != nil {
		s.log.ErrorContext(ctx, "Deploy: failed", "err", err)
		return nil, fmt.Errorf("deploy: %w", err)
	}
	cleanedUp = true
	return &mwanv1.DeployResponse{
		StagedSha256:  stagedSHA256,
		PreviousPath:  previousPath,
		ReExecStarted: true,
	}, nil
}

// DeployStatus returns current deploy state. With Mark=MARK_HEALTHY,
// it also clears the pending-verify marker and stamps health=ok.
func (s *Server) DeployStatus(ctx context.Context, req *mwanv1.DeployStatusRequest) (*mwanv1.DeployStatusResponse, error) {
	s.log.InfoContext(ctx, "opnsensesvc: DeployStatus", "mark", req.GetMark().String())

	if s.deploy == nil {
		return nil, errors.New("DeployStatus: DeployManager not configured")
	}

	if req.GetMark() == mwanv1.DeployStatusRequest_MARK_HEALTHY {
		active, previous, deployedAt, err := s.deploy.MarkHealthy(ctx)
		if err != nil {
			s.log.ErrorContext(ctx, "DeployStatus: mark healthy failed", "err", err)
			return nil, fmt.Errorf("mark healthy: %w", err)
		}
		return &mwanv1.DeployStatusResponse{
			ActiveSha256:   active,
			PreviousSha256: previous,
			Health:         HealthOK,
			DeployedAt:     deployedAt,
		}, nil
	}

	// Read-only path.
	active, previous, health, deployedAt, err := s.deploy.Status(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "DeployStatus: read failed", "err", err)
		return nil, fmt.Errorf("status: %w", err)
	}
	return &mwanv1.DeployStatusResponse{
		ActiveSha256:   active,
		PreviousSha256: previous,
		Health:         health,
		DeployedAt:     deployedAt,
	}, nil
}

// Revert copies .previous to .current, drops pending-verify marker,
// and re-execs.
func (s *Server) Revert(ctx context.Context, _ *mwanv1.RevertRequest) (*mwanv1.RevertResponse, error) {
	s.log.InfoContext(ctx, "opnsensesvc: Revert")

	if s.deploy == nil {
		return nil, errors.New("Revert: DeployManager not configured")
	}
	revertedTo, err := s.deploy.Revert(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "Revert: failed", "err", err)
		return nil, fmt.Errorf("revert: %w", err)
	}
	return &mwanv1.RevertResponse{
		RevertedToSha256: revertedTo,
		ReExecStarted:    true,
	}, nil
}

// ServeOpts controls the dispatcher loop.
type ServeOpts struct {
	SerialPath   string
	OpenSerial   func(path string) (io.ReadWriteCloser, error)
	Server       *Server
	Log          *slog.Logger
	OnSerialOpen func(path string)
	// LongSerialPath, when non-empty, enables the channel-split mode.
	// A second dispatcher attaches to this device and accepts only
	// LongChannelMethods. The primary dispatcher then restricts itself
	// to ShortChannelMethods so a wedge on the long port cannot starve
	// short RPCs. Leaving this empty preserves the original single-port
	// behavior: the primary dispatcher accepts every method.
	LongSerialPath string
}

// Serve opens the virtio-serial device and runs the MWN1 dispatcher
// against it until ctx is cancelled. There is no TLS and no
// application-level authentication; the only peer is the host process
// that owns the unix socket on the Proxmox side.
func Serve(ctx context.Context, opts ServeOpts) error {
	if opts.Server == nil {
		return errors.New("Serve: Server required")
	}
	if opts.SerialPath == "" {
		return errors.New("Serve: SerialPath required")
	}
	if opts.OpenSerial == nil {
		return errors.New("Serve: OpenSerial required")
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	short, err := openShortDispatcher(ctx, opts, log)
	if err != nil {
		return err
	}
	defer short.close()

	long, err := openLongDispatcher(ctx, opts, log)
	if err != nil {
		return err
	}
	defer long.close()

	dispatcherCtx, dispatcherCancel := context.WithCancel(ctx)
	defer dispatcherCancel()

	longErrCh := startLongServe(dispatcherCtx, long, log)

	serveErr := short.dispatcher.Serve(dispatcherCtx, short.rw)
	dispatcherCancel()
	longServeErr := <-longErrCh

	return collectServeErrors(ctx, log, serveErr, longServeErr)
}

// dispatcherSession bundles a dispatcher with the [io.ReadWriteCloser]
// it owns so the channel-split branches can hand both around without
// repeating themselves. Cleanup closes the RW; the dispatcher itself
// holds no resources that need explicit Close.
type dispatcherSession struct {
	dispatcher *Dispatcher
	rw         io.ReadWriteCloser
}

func (s *dispatcherSession) close() {
	if s == nil || s.rw == nil {
		return
	}
	_ = s.rw.Close()
}

func openShortDispatcher(ctx context.Context, opts ServeOpts, log *slog.Logger) (*dispatcherSession, error) {
	rw, err := opts.OpenSerial(opts.SerialPath)
	if err != nil {
		log.ErrorContext(ctx, "opnsensesvc: open serial failed",
			"err", err, "path", opts.SerialPath)
		return nil, fmt.Errorf("opnsensesvc: open serial: %w", err)
	}
	if opts.OnSerialOpen != nil {
		opts.OnSerialOpen(opts.SerialPath)
	}
	log.InfoContext(ctx, "opnsensesvc: serial opened", "path", opts.SerialPath)
	var allowed []uint16
	if opts.LongSerialPath != "" {
		allowed = ShortChannelMethods
	}
	d, err := NewDispatcher(DispatcherConfig{
		Registry:       nil,
		Server:         opts.Server,
		Workers:        0,
		Log:            log,
		OnFrameError:   nil,
		AllowedMethods: allowed,
	})
	if err != nil {
		_ = rw.Close()
		log.ErrorContext(ctx, "opnsensesvc: build dispatcher failed", "err", err)
		return nil, fmt.Errorf("opnsensesvc: build dispatcher: %w", err)
	}
	return &dispatcherSession{dispatcher: d, rw: rw}, nil
}

func openLongDispatcher(ctx context.Context, opts ServeOpts, log *slog.Logger) (*dispatcherSession, error) {
	if opts.LongSerialPath == "" {
		return &dispatcherSession{dispatcher: nil, rw: nil}, nil
	}
	rw, err := opts.OpenSerial(opts.LongSerialPath)
	if err != nil {
		log.ErrorContext(ctx, "opnsensesvc: open long serial failed",
			"err", err, "path", opts.LongSerialPath)
		return nil, fmt.Errorf("opnsensesvc: open long serial: %w", err)
	}
	if opts.OnSerialOpen != nil {
		opts.OnSerialOpen(opts.LongSerialPath)
	}
	log.InfoContext(ctx, "opnsensesvc: long serial opened", "path", opts.LongSerialPath)
	d, err := NewDispatcher(DispatcherConfig{
		Registry:       nil,
		Server:         opts.Server,
		Workers:        0,
		Log:            log,
		OnFrameError:   nil,
		AllowedMethods: LongChannelMethods,
	})
	if err != nil {
		_ = rw.Close()
		log.ErrorContext(ctx, "opnsensesvc: build long dispatcher failed", "err", err)
		return nil, fmt.Errorf("opnsensesvc: build long dispatcher: %w", err)
	}
	return &dispatcherSession{dispatcher: d, rw: rw}, nil
}

func startLongServe(ctx context.Context, sess *dispatcherSession, log *slog.Logger) <-chan error {
	ch := make(chan error, 1)
	if sess == nil || sess.dispatcher == nil {
		ch <- nil
		return ch
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(ctx, "opnsensesvc: long serve goroutine panic",
					"err", fmt.Errorf("recovered panic: %v", recovered))
			}
		}()
		ch <- sess.dispatcher.Serve(ctx, sess.rw)
	}()
	return ch
}

func collectServeErrors(ctx context.Context, log *slog.Logger, serveErr, longServeErr error) error {
	if serveErr != nil {
		log.ErrorContext(ctx, "opnsensesvc: dispatcher serve failed", "err", serveErr)
		return fmt.Errorf("opnsensesvc: dispatcher serve: %w", serveErr)
	}
	if longServeErr != nil {
		log.ErrorContext(ctx, "opnsensesvc: long dispatcher serve failed", "err", longServeErr)
		return fmt.Errorf("opnsensesvc: long dispatcher serve: %w", longServeErr)
	}
	return nil
}
