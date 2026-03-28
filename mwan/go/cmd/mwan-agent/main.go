// Command mwan-agent serves the MWAN gRPC API on virtio-vsock (default port 50051).
package main

import (
	"context"
	"log"
	"os"
	"strconv"

	mwanv1 "github.com/agoodkind/infra-tools/gen/mwan/v1"
	"github.com/mdlayher/vsock"
	"google.golang.org/grpc"
)

const defaultListenPort = 50051

func main() {
	port := uint32(defaultListenPort)
	if p := os.Getenv("MWAN_AGENT_VSOCK_PORT"); p != "" {
		if n, err := strconv.ParseUint(p, 10, 32); err == nil {
			port = uint32(n)
		}
	}
	lis, err := vsock.Listen(port, nil)
	if err != nil {
		log.Fatalf("vsock listen: %v", err)
	}
	srv := grpc.NewServer()
	mwanv1.RegisterMWANAgentServer(srv, &agentServer{})
	log.Printf("mwan-agent listening on vsock port %d", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("grpc: %v", err)
	}
}

type agentServer struct {
	mwanv1.UnimplementedMWANAgentServer
}

func (a *agentServer) GetHealth(
	ctx context.Context,
	_ *mwanv1.GetHealthRequest,
) (*mwanv1.GetHealthResponse, error) {
	return &mwanv1.GetHealthResponse{}, nil
}

func (a *agentServer) Ping(
	ctx context.Context,
	_ *mwanv1.PingRequest,
) (*mwanv1.PingResponse, error) {
	return &mwanv1.PingResponse{}, nil
}

func (a *agentServer) GetConfigState(
	ctx context.Context,
	_ *mwanv1.GetConfigStateRequest,
) (*mwanv1.GetConfigStateResponse, error) {
	return &mwanv1.GetConfigStateResponse{}, nil
}

func (a *agentServer) GetSystemInfo(
	ctx context.Context,
	_ *mwanv1.GetSystemInfoRequest,
) (*mwanv1.GetSystemInfoResponse, error) {
	h, _ := os.Hostname()
	return &mwanv1.GetSystemInfoResponse{Hostname: h}, nil
}

func (a *agentServer) WatchEvents(
	_ *mwanv1.WatchEventsRequest,
	stream mwanv1.MWANAgent_WatchEventsServer,
) error {
	<-stream.Context().Done()
	return stream.Context().Err()
}
