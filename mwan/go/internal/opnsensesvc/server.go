package opnsensesvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/version"
)

// Server implements mwanv1.MWANOPNsenseServiceServer.
//
// All write operations on /conf/config.xml take a snapshot first via
// backupConfig and return the backup path, so a client can immediately
// revert by calling WriteConfigXML with the snapshot bytes.
type Server struct {
	mwanv1.UnimplementedMWANOPNsenseServiceServer

	configPath string
	backupDir  string
	log        *slog.Logger
	clock      Clock
	mu         sync.Mutex // serializes mutating ops on config.xml
}

// NewServer constructs a Server. configPath defaults to ConfigPath
// when empty; backupDir defaults to BackupDir when empty.
func NewServer(log *slog.Logger, configPath, backupDir string) *Server {
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
	}
}

// Register registers s on grpcServer.
func (s *Server) Register(grpcServer *grpc.Server) {
	mwanv1.RegisterMWANOPNsenseServiceServer(grpcServer, s)
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
	peerInfo, _ := peer.FromContext(ctx)
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: Exec", peerInfo,
		slog.String("exec_command", req.GetCommand()),
		slog.Int("args_len", len(req.GetArgs())),
		slog.Bool("sudo", req.GetSudo()),
		slog.Int64("timeout_seconds", int64(req.GetTimeoutSeconds())))

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
		serverLogAttrs(ctx, s.log, slog.LevelError, "opnsensesvc: Exec failed", peerInfo,
			slog.Any("err", err),
			slog.String("exec_command", req.GetCommand()))
		return nil, status.Errorf(codes.Internal, "exec: %v", err)
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
	peerInfo, _ := peer.FromContext(ctx)
	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		serverLogAttrs(ctx, s.log, slog.LevelError, "opnsensesvc: ReadConfigXML failed", peerInfo,
			slog.Any("err", err),
			slog.String("path", s.configPath))
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	sum := sha256.Sum256(content)
	encodedSum := hex.EncodeToString(sum[:])
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: ReadConfigXML", peerInfo,
		slog.Int("size_bytes", len(content)),
		slog.String("sha256", encodedSum))
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
	peerInfo, _ := peer.FromContext(ctx)
	if len(req.GetContent()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "content empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, req.GetLabel())
	if err != nil {
		serverLogAttrs(ctx, s.log, slog.LevelError,
			"opnsensesvc: WriteConfigXML backup failed", peerInfo,
			slog.Any("err", err),
			slog.String("label", req.GetLabel()))
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfigWithLog(ctx, s.log, s.configPath, req.GetContent()); err != nil {
		serverLogAttrs(ctx, s.log, slog.LevelError,
			"opnsensesvc: WriteConfigXML write failed", peerInfo,
			slog.Any("err", err),
			slog.String("backup_path", backupPath))
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: WriteConfigXML", peerInfo,
		slog.String("backup_path", backupPath),
		slog.Int("bytes_written", len(req.GetContent())))
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
	peerInfo, _ := peer.FromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()

	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, req.GetLabel())
	if err != nil {
		serverLogAttrs(ctx, s.log, slog.LevelError,
			"opnsensesvc: BackupConfigXML failed", peerInfo,
			slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	content, err := readConfigWithLog(ctx, s.log, backupPath)
	if err != nil {
		serverLogAttrs(ctx, s.log, slog.LevelError,
			"opnsensesvc: BackupConfigXML stat failed", peerInfo,
			slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "stat: %v", err)
	}
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: BackupConfigXML", peerInfo,
		slog.String("backup_path", backupPath),
		slog.Int("size_bytes", len(content)))
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
	peerInfo, _ := peer.FromContext(ctx)
	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	matches, err := xpathGetWithLog(ctx, s.log, content, req.GetExpression())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "xpath: %v", err)
	}
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: XPathGet", peerInfo,
		slog.String("expression", req.GetExpression()),
		slog.Int("matches", len(matches)))
	return &mwanv1.XPathGetResponse{Matches: matches}, nil
}

// XPathSet snapshots first, applies the change, writes atomically.
func (s *Server) XPathSet(
	ctx context.Context,
	req *mwanv1.XPathSetRequest,
) (*mwanv1.XPathSetResponse, error) {
	peerInfo, _ := peer.FromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	updated, n, err := xpathSetWithLog(ctx, s.log,
		content, req.GetExpression(), req.GetNewValue())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "xpath: %v", err)
	}
	if n == 0 {
		return &mwanv1.XPathSetResponse{ChangedCount: 0}, nil
	}
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, "xpath-set")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfigWithLog(ctx, s.log, s.configPath, updated); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: XPathSet", peerInfo,
		slog.String("expression", req.GetExpression()),
		slog.Int("changed_count", n),
		slog.String("backup_path", backupPath))
	return &mwanv1.XPathSetResponse{
		BackupPath:   backupPath,
		ChangedCount: int32(n),
	}, nil
}

// XPathDelete snapshots first, deletes matching nodes, writes
// atomically.
func (s *Server) XPathDelete(
	ctx context.Context,
	req *mwanv1.XPathDeleteRequest,
) (*mwanv1.XPathDeleteResponse, error) {
	peerInfo, _ := peer.FromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	updated, n, err := xpathDeleteWithLog(ctx, s.log, content, req.GetExpression())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "xpath: %v", err)
	}
	if n == 0 {
		return &mwanv1.XPathDeleteResponse{DeletedCount: 0}, nil
	}
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, "xpath-delete")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfigWithLog(ctx, s.log, s.configPath, updated); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: XPathDelete", peerInfo,
		slog.String("expression", req.GetExpression()),
		slog.Int("deleted_count", n),
		slog.String("backup_path", backupPath))
	return &mwanv1.XPathDeleteResponse{
		BackupPath:   backupPath,
		DeletedCount: int32(n),
	}, nil
}

// StripGatewayV6 is the convenience wrapper for the cutover path.
func (s *Server) StripGatewayV6(
	ctx context.Context,
	_ *mwanv1.StripGatewayV6Request,
) (*mwanv1.StripGatewayV6Response, error) {
	peerInfo, _ := peer.FromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	updated, changed, err := stripGatewayV6WithLog(ctx, s.log, content)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "strip: %v", err)
	}
	if !changed {
		return &mwanv1.StripGatewayV6Response{Changed: false}, nil
	}
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, "strip-gatewayv6")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfigWithLog(ctx, s.log, s.configPath, updated); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: StripGatewayV6", peerInfo,
		slog.String("backup_path", backupPath))
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
	peerInfo, _ := peer.FromContext(ctx)
	if req.GetGatewayName() == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfigWithLog(ctx, s.log, s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	updated, changed, err := injectGatewayV6WithLog(ctx, s.log,
		content, req.GetGatewayName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "inject: %v", err)
	}
	if !changed {
		return &mwanv1.InjectGatewayV6Response{Changed: false}, nil
	}
	backupPath, err := backupConfigWithLog(ctx, s.log, s.clock,
		s.configPath, s.backupDir, "inject-gatewayv6")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfigWithLog(ctx, s.log, s.configPath, updated); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	serverLogAttrs(ctx, s.log, slog.LevelInfo, "opnsensesvc: InjectGatewayV6", peerInfo,
		slog.String("backup_path", backupPath),
		slog.String("gateway_name", req.GetGatewayName()))
	return &mwanv1.InjectGatewayV6Response{
		BackupPath: backupPath,
		Changed:    true,
	}, nil
}

// ServeOpts controls the listener loop.
type ServeOpts struct {
	SerialPath   string
	OpenSerial   func(path string) (io.ReadWriteCloser, error)
	Server       *Server
	Log          *slog.Logger
	OnSerialOpen func(path string)
}

// Serve runs the virtio-serial listener until ctx is cancelled. There
// is no TLS and no application-level authentication; the only peer is
// the host process that owns the unix socket on the Proxmox side.
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

	gs := grpc.NewServer()
	opts.Server.Register(gs)

	serLis, err := NewSerialListener(opts.SerialPath, opts.OpenSerial)
	if err != nil {
		return fmt.Errorf("opnsensesvc: open serial listener: %w", err)
	}
	serLis.log = log
	if opts.OnSerialOpen != nil {
		opts.OnSerialOpen(opts.SerialPath)
	}
	log.InfoContext(ctx, "opnsensesvc: serial listening", "path", opts.SerialPath)

	errCh := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				recoveredErr := fmt.Errorf("serial serve panic: %v", recovered)
				log.ErrorContext(ctx,
					"opnsensesvc: serial serve panic recovered",
					"recovered", recovered,
					"err", recoveredErr)
				errCh <- recoveredErr
			}
		}()
		err := gs.Serve(serLis)
		if err == nil {
			errCh <- nil
			return
		}
		errCh <- fmt.Errorf("serial serve: %w", err)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			gs.Stop()
		}
		return err
	case <-ctx.Done():
		log.InfoContext(ctx, "opnsensesvc: context cancelled, stopping gRPC")
		if err := serLis.Close(); err != nil {
			log.ErrorContext(ctx,
				"opnsensesvc: serial listener close failed",
				"err", err)
		}
		gs.Stop()
		err := <-errCh
		if err != nil {
			log.InfoContext(ctx,
				"opnsensesvc: serial serve stopped after context cancel",
				"err", err)
		}
		return nil
	}
}
