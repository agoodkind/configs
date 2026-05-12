package opnsensesvc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Serve opens the virtio-serial device, wraps it in a one-shot
// listener, registers OpnsenseService and TransferService against a
// new gRPC server, and serves until ctx is cancelled. WriteBufferSize
// is set to 0 so HTTP/2 frames are not coalesced past the FreeBSD
// tty input queue ceiling.
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
	if opts.Transfer != nil {
		opts.Server.AttachTransferManager(opts.Transfer)
	}

	rwc, err := opts.OpenSerial(opts.SerialPath)
	if err != nil {
		return logWrappedErrorContext(ctx, log,
			"opnsensesvc: open serial", "opnsensesvc: open serial", err,
			slog.String("path", opts.SerialPath))
	}
	if opts.OnSerialOpen != nil {
		opts.OnSerialOpen(opts.SerialPath)
	}
	log.InfoContext(ctx, "opnsensesvc: serial opened", "path", opts.SerialPath)

	listener := NewOneShotListener(rwc)
	grpcServer := grpc.NewServer(
		grpc.WriteBufferSize(0),
		grpc.Creds(insecure.NewCredentials()),
	)
	mwanv1.RegisterOpnsenseServiceServer(grpcServer, opts.Server)
	mwanv1.RegisterTransferServiceServer(grpcServer, opts.Server)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.ErrorContext(ctx, "opnsensesvc: stop watcher panic",
					"panic", r,
					"err", fmt.Errorf("panic: %v", r))
			}
		}()
		<-ctx.Done()
		log.InfoContext(ctx, "opnsensesvc: stopping gRPC server")
		grpcServer.GracefulStop()
		_ = listener.Shutdown(ctx)
	}()

	serveErr := grpcServer.Serve(listener)
	if serveErr != nil && !errors.Is(serveErr, net.ErrClosed) && !errors.Is(serveErr, grpc.ErrServerStopped) {
		return logWrappedErrorContext(ctx, log,
			"opnsensesvc: grpc serve", "opnsensesvc: grpc serve", serveErr)
	}
	return nil
}
