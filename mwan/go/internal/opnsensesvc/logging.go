package opnsensesvc

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"google.golang.org/grpc/peer"
)

func loggerOrDefault(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.Default()
}

func logWrappedErrorContext(
	ctx context.Context,
	log *slog.Logger,
	logMessage string,
	wrapMessage string,
	err error,
	attrs ...slog.Attr,
) error {
	logger := loggerOrDefault(log).With(attrsFromSlice(attrs)...)
	logger.ErrorContext(ctx, logMessage, "err", err)
	return fmt.Errorf("%s: %w", wrapMessage, err)
}

func attrsFromSlice(attrs []slog.Attr) []any {
	values := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		values = append(values, attr)
	}
	return values
}

func logCloseErrorContext(
	ctx context.Context,
	log *slog.Logger,
	closer io.Closer,
	message string,
	attrs ...slog.Attr,
) {
	if err := closer.Close(); err != nil {
		loggerOrDefault(log).LogAttrs(
			ctx,
			slog.LevelWarn,
			message,
			append(attrs, slog.Any("err", err))...,
		)
	}
}

func grpcPeerLogAttrs(peerInfo *peer.Peer) []slog.Attr {
	if peerInfo == nil || peerInfo.Addr == nil {
		return []slog.Attr{
			slog.String("peer_addr", ""),
			slog.String("transport", ""),
		}
	}
	return []slog.Attr{
		slog.String("peer_addr", peerInfo.Addr.String()),
		slog.String("transport", peerInfo.Addr.Network()),
	}
}

func serverLogAttrs(
	ctx context.Context,
	log *slog.Logger,
	level slog.Level,
	message string,
	peerInfo *peer.Peer,
	attrs ...slog.Attr,
) {
	allAttrs := grpcPeerLogAttrs(peerInfo)
	allAttrs = append(allAttrs, attrs...)
	loggerOrDefault(log).LogAttrs(ctx, level, message, allAttrs...)
}
