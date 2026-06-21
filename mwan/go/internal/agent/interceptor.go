package agent

import (
	"context"
	"log/slog"
	"path"
	"strings"

	"goodkind.io/mwan/internal/tracing"
	"google.golang.org/grpc/metadata"
	grpcstats "google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

var traceMetadataKeys = []string{
	"x-trace-id",
	"trace-id",
	"trace_id",
}

type traceStatsHandler struct {
	logger *slog.Logger
}

func newTraceStatsHandler(
	logger *slog.Logger,
) *traceStatsHandler {
	return &traceStatsHandler{logger: logger}
}

func (h *traceStatsHandler) TagConn(
	ctx context.Context,
	info *grpcstats.ConnTagInfo,
) context.Context {
	if info == nil || info.RemoteAddr == nil {
		return ctx
	}
	return tracing.WithAttrs(ctx,
		slog.String("peer_addr", info.RemoteAddr.String()),
		slog.String("transport", info.RemoteAddr.Network()),
	)
}

func (h *traceStatsHandler) HandleConn(context.Context, grpcstats.ConnStats) {}

func (h *traceStatsHandler) TagRPC(
	ctx context.Context,
	info *grpcstats.RPCTagInfo,
) context.Context {
	if info == nil {
		return ctx
	}
	methodName := path.Base(info.FullMethodName)
	traceID := incomingTraceID(ctx)
	if traceID == "" {
		traceID = tracing.NewID()
	}
	ctx = tracing.WithTraceID(ctx, traceID)
	ctx = tracing.WithComponent(ctx, "agent")
	ctx = tracing.WithOperation(ctx, methodName)
	ctx = tracing.WithEvent(ctx, "grpc_request")
	ctx = tracing.WithAttrs(ctx, slog.String("rpc_method", info.FullMethodName))
	ctx, _ = tracing.StartTrace(ctx, "", methodName)
	return ctx
}

func (h *traceStatsHandler) HandleRPC(ctx context.Context, rpcStats grpcstats.RPCStats) {
	if rpcStats.IsClient() {
		return
	}
	log := tracing.Logger(ctx, h.logger)
	switch stat := rpcStats.(type) {
	case *grpcstats.Begin:
		log.InfoContext(ctx, "grpc request started")
	case *grpcstats.End:
		log.InfoContext(
			ctx,
			"grpc request finished",
			"grpc_code", status.Code(stat.Error).String(),
			"duration_ms", stat.EndTime.Sub(stat.BeginTime).Milliseconds(),
		)
	}
}

func incomingTraceID(ctx context.Context) string {
	metadataMap, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, key := range traceMetadataKeys {
		values := metadataMap.Get(key)
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
