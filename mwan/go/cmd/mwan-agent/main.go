// Command mwan-agent serves the MWAN gRPC API on TCP and optionally virtio-vsock.
package main

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	mwanv1 "github.com/agoodkind/infra-tools/gen/mwan/v1"
	"github.com/mdlayher/vsock"
	"google.golang.org/grpc"
)

func main() {
	vsockPort := flag.Uint("vsock-port", 50051, "virtio-vsock listen port (0 disables)")
	tcpAddr := flag.String("tcp-addr", "[::]:50052", "TCP listen address for gRPC")
	deployFile := flag.String(
		"deploy-file",
		"/var/run/mwan-last-deploy",
		"path to last deploy timestamp file",
	)
	logFile := flag.String("log-file", "/var/log/mwan-agent.log", "path to text log file")
	flag.Parse()

	logger, err := newAgentLogger(*logFile)
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
		"detail", buildVersionString(),
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
	mwanv1.RegisterMWANAgentServer(grpcServer, newAgentServer(*deployFile, logger))

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
