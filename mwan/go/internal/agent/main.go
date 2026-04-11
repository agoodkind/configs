// Package agent provides the gRPC agent for serving the MWAN API.
package agent

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/logging"
	"github.com/mdlayher/vsock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Run is the entry point for the agent subcommand.
// It parses flags, sets up logging, and starts the gRPC server.
func Run(cfg *config.Config) {
	vsockPort := flag.Uint("vsock-port", uint(cfg.Agent.VsockPort), "virtio-vsock listen port (0 disables)")
	tcpAddr := flag.String("tcp-addr", cfg.Agent.TCPAddr, "TCP listen address for gRPC")
	deployFile := flag.String(
		"deploy-file",
		cfg.Agent.DeployFile,
		"path to last deploy timestamp file",
	)
	logFile := flag.String("log-file", cfg.Agent.LogFile, "path to JSON log file")
	debug := flag.Bool(
		"debug",
		cfg.Agent.Debug,
		"enable gRPC reflection service for ad-hoc grpcurl inspection",
	)
	flag.Parse()

	logger, err := logging.New(logging.Config{
		JSONLogFile: *logFile,
	}, buildVersion())
	if err != nil {
		boot := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		boot.Error("init logger", "error", err, "log_file", *logFile)
		os.Exit(1)
	}

	if *vsockPort != 0 && *vsockPort > 0xffffffff {
		logger.Error("vsock port out of range", "vsock_port", *vsockPort)
		os.Exit(1)
	}
	port := uint32(*vsockPort)

	logger.Info(
		"mwan-agent starting",
		"detail", buildVersion(),
		"vsock_port", *vsockPort,
		"tcp_addr", *tcpAddr,
	)

	var vsockLis net.Listener
	if port != 0 {
		var vsockErr error
		vsockLis, vsockErr = vsock.Listen(port, nil)
		if vsockErr != nil {
			logger.Warn(
				"vsock listen failed, continuing without vsock",
				"error", vsockErr,
				"vsock_port", port,
			)
		}
	}

	tcpLis, err := net.Listen("tcp", *tcpAddr)
	if err != nil {
		logger.Error("tcp listen", "error", err, "tcp_addr", *tcpAddr)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	mwanv1.RegisterMWANAgentServer(grpcServer, NewServer(*deployFile, logger))
	if *debug {
		reflection.Register(grpcServer)
		logger.Info("gRPC reflection enabled (debug mode)")
	}

	serveCount := 1
	if vsockLis != nil {
		serveCount++
	}

	errCh := make(chan error, 2)

	go func() {
		errCh <- grpcServer.Serve(tcpLis)
	}()
	if vsockLis != nil {
		go func() {
			errCh <- grpcServer.Serve(vsockLis)
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("grpc serve", "error", err)
			os.Exit(1)
		}
		for i := 1; i < serveCount; i++ {
			if err := <-errCh; err != nil {
				logger.Error("grpc serve", "error", err)
				os.Exit(1)
			}
		}
	case sig := <-sigCh:
		logger.Info("shutdown signal", "signal", sig.String())
		grpcServer.GracefulStop()
		for i := 0; i < serveCount; i++ {
			if err := <-errCh; err != nil {
				logger.Error("grpc after graceful stop", "error", err)
				os.Exit(1)
			}
		}
	}
}

// buildVersion returns the build version string.
// This should match the version.go implementation from cmd/mwan/.
func buildVersion() string {
	// TODO: import from a shared location or pass in
	return "dev"
}
