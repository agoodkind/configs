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
	s.log.InfoContext(ctx, "opnsensesvc: Exec",
		"command", req.GetCommand(),
		"args_len", len(req.GetArgs()),
		"sudo", req.GetSudo(),
		"timeout_seconds", req.GetTimeoutSeconds())

	res, err := runExec(ctx, ExecArgs{
		Command:        req.GetCommand(),
		Args:           req.GetArgs(),
		Sudo:           req.GetSudo(),
		TimeoutSeconds: req.GetTimeoutSeconds(),
		StdinBytes:     req.GetStdinBytes(),
	})
	if err != nil {
		s.log.ErrorContext(ctx, "opnsensesvc: Exec failed",
			"err", err,
			"command", req.GetCommand())
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
func (s *Server) ReadConfigXML(_ context.Context, _ *mwanv1.ReadConfigXMLRequest) (*mwanv1.ReadConfigXMLResponse, error) {
	content, err := readConfig(s.configPath)
	if err != nil {
		s.log.Error("opnsensesvc: ReadConfigXML failed", "err", err, "path", s.configPath)
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	sum := sha256.Sum256(content)
	s.log.Info("opnsensesvc: ReadConfigXML",
		"size_bytes", len(content),
		"sha256", hex.EncodeToString(sum[:]))
	return &mwanv1.ReadConfigXMLResponse{
		Content:   content,
		SizeBytes: int64(len(content)),
		Sha256:    hex.EncodeToString(sum[:]),
	}, nil
}

// WriteConfigXML snapshots the current config first, then writes
// the new content atomically.
func (s *Server) WriteConfigXML(_ context.Context, req *mwanv1.WriteConfigXMLRequest) (*mwanv1.WriteConfigXMLResponse, error) {
	if len(req.GetContent()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "content empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	backupPath, err := backupConfig(s.configPath, s.backupDir, req.GetLabel())
	if err != nil {
		s.log.Error("opnsensesvc: WriteConfigXML backup failed",
			"err", err,
			"label", req.GetLabel())
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfig(s.configPath, req.GetContent()); err != nil {
		s.log.Error("opnsensesvc: WriteConfigXML write failed",
			"err", err,
			"backup_path", backupPath)
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	s.log.Info("opnsensesvc: WriteConfigXML",
		"backup_path", backupPath,
		"bytes_written", len(req.GetContent()))
	return &mwanv1.WriteConfigXMLResponse{
		BackupPath:   backupPath,
		BytesWritten: int64(len(req.GetContent())),
	}, nil
}

// BackupConfigXML takes an explicit snapshot.
func (s *Server) BackupConfigXML(_ context.Context, req *mwanv1.BackupConfigXMLRequest) (*mwanv1.BackupConfigXMLResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	backupPath, err := backupConfig(s.configPath, s.backupDir, req.GetLabel())
	if err != nil {
		s.log.Error("opnsensesvc: BackupConfigXML failed", "err", err)
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	content, err := readConfig(backupPath)
	if err != nil {
		s.log.Error("opnsensesvc: BackupConfigXML stat failed", "err", err)
		return nil, status.Errorf(codes.Internal, "stat: %v", err)
	}
	s.log.Info("opnsensesvc: BackupConfigXML", "backup_path", backupPath, "size_bytes", len(content))
	return &mwanv1.BackupConfigXMLResponse{
		BackupPath: backupPath,
		SizeBytes:  int64(len(content)),
	}, nil
}

// XPathGet runs a read-only XPath query.
func (s *Server) XPathGet(_ context.Context, req *mwanv1.XPathGetRequest) (*mwanv1.XPathGetResponse, error) {
	content, err := readConfig(s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	matches, err := xpathGet(content, req.GetExpression())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "xpath: %v", err)
	}
	s.log.Info("opnsensesvc: XPathGet",
		"expression", req.GetExpression(),
		"matches", len(matches))
	return &mwanv1.XPathGetResponse{Matches: matches}, nil
}

// XPathSet snapshots first, applies the change, writes atomically.
func (s *Server) XPathSet(_ context.Context, req *mwanv1.XPathSetRequest) (*mwanv1.XPathSetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfig(s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	updated, n, err := xpathSet(content, req.GetExpression(), req.GetNewValue())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "xpath: %v", err)
	}
	if n == 0 {
		return &mwanv1.XPathSetResponse{ChangedCount: 0}, nil
	}
	backupPath, err := backupConfig(s.configPath, s.backupDir, "xpath-set")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfig(s.configPath, updated); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	s.log.Info("opnsensesvc: XPathSet",
		"expression", req.GetExpression(),
		"changed_count", n,
		"backup_path", backupPath)
	return &mwanv1.XPathSetResponse{
		BackupPath:   backupPath,
		ChangedCount: int32(n),
	}, nil
}

// XPathDelete snapshots first, deletes matching nodes, writes
// atomically.
func (s *Server) XPathDelete(_ context.Context, req *mwanv1.XPathDeleteRequest) (*mwanv1.XPathDeleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfig(s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	updated, n, err := xpathDelete(content, req.GetExpression())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "xpath: %v", err)
	}
	if n == 0 {
		return &mwanv1.XPathDeleteResponse{DeletedCount: 0}, nil
	}
	backupPath, err := backupConfig(s.configPath, s.backupDir, "xpath-delete")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfig(s.configPath, updated); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	s.log.Info("opnsensesvc: XPathDelete",
		"expression", req.GetExpression(),
		"deleted_count", n,
		"backup_path", backupPath)
	return &mwanv1.XPathDeleteResponse{
		BackupPath:   backupPath,
		DeletedCount: int32(n),
	}, nil
}

// StripGatewayV6 is the convenience wrapper for the cutover path.
func (s *Server) StripGatewayV6(_ context.Context, _ *mwanv1.StripGatewayV6Request) (*mwanv1.StripGatewayV6Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfig(s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	updated, changed, err := stripGatewayV6(content)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "strip: %v", err)
	}
	if !changed {
		return &mwanv1.StripGatewayV6Response{Changed: false}, nil
	}
	backupPath, err := backupConfig(s.configPath, s.backupDir, "strip-gatewayv6")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfig(s.configPath, updated); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	s.log.Info("opnsensesvc: StripGatewayV6", "backup_path", backupPath)
	return &mwanv1.StripGatewayV6Response{
		BackupPath: backupPath,
		Changed:    true,
	}, nil
}

// InjectGatewayV6 is the convenience wrapper for the cutover unfuck
// path.
func (s *Server) InjectGatewayV6(_ context.Context, req *mwanv1.InjectGatewayV6Request) (*mwanv1.InjectGatewayV6Response, error) {
	if req.GetGatewayName() == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := readConfig(s.configPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read: %v", err)
	}
	updated, changed, err := injectGatewayV6(content, req.GetGatewayName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "inject: %v", err)
	}
	if !changed {
		return &mwanv1.InjectGatewayV6Response{Changed: false}, nil
	}
	backupPath, err := backupConfig(s.configPath, s.backupDir, "inject-gatewayv6")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "backup: %v", err)
	}
	if err := writeConfig(s.configPath, updated); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}
	s.log.Info("opnsensesvc: InjectGatewayV6",
		"backup_path", backupPath,
		"gateway_name", req.GetGatewayName())
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
// Returns the listener's error (gRPC stops on context cancel).
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

	serLis := NewSerialListener(opts.SerialPath, opts.OpenSerial)
	if opts.OnSerialOpen != nil {
		opts.OnSerialOpen(opts.SerialPath)
	}
	log.Info("opnsensesvc: serial listening", "path", opts.SerialPath)

	errCh := make(chan error, 1)
	go func() {
		err := gs.Serve(serLis)
		errCh <- fmt.Errorf("serial serve: %w", err)
	}()

	go func() {
		<-ctx.Done()
		log.Info("opnsensesvc: context cancelled, stopping gRPC")
		gs.GracefulStop()
	}()

	err := <-errCh
	gs.GracefulStop()
	return err
}
