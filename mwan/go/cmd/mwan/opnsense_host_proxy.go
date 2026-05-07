package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"goodkind.io/mwan/internal/mwn1"
)

type corrRoute struct {
	inbound     *inboundConn
	inboundCorr uint64
	methodID    uint16
	streaming   bool
	forwarder   *streamForwarder
}

type streamForwardJob struct {
	methodID     uint16
	inboundCorr  uint64
	upstreamCorr uint64
	flags        mwn1.Flags
	payload      []byte
}

type streamForwarder struct {
	cancel context.CancelFunc
	done   <-chan struct{}
	jobs   chan streamForwardJob
}

type inboundConn struct {
	id     uint64
	conn   *mwn1.Conn
	bridge *fanInBridge

	mu      sync.Mutex
	pending map[uint64]uint64
}

type fanInBridge struct {
	upstream *mwn1.Conn
	listener net.Listener
	log      *slog.Logger

	nextConnID       atomic.Uint64
	nextUpstreamCorr atomic.Uint64

	mu       sync.Mutex
	routes   map[uint64]corrRoute
	inbounds map[uint64]*inboundConn
}

func newFanInBridge(upstream io.ReadWriteCloser, listener net.Listener, log *slog.Logger) *fanInBridge {
	if log == nil {
		log = slog.Default()
	}
	bridge := &fanInBridge{
		listener: listener,
		log:      log,
		routes:   make(map[uint64]corrRoute),
		inbounds: make(map[uint64]*inboundConn),
	}
	bridge.upstream = mwn1.NewConn(upstream, log)
	bridge.upstream.OnMessage(bridge.handleUpstreamMessage)
	return bridge
}

func (b *fanInBridge) serve(ctx context.Context) error {
	acceptErrCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				b.log.ErrorContext(ctx, "mwan-opnsense-host: accept loop panic recovered",
					slog.Any("err", err))
				acceptErrCh <- err
			}
		}()
		acceptErrCh <- b.acceptLoop(ctx)
	}()

	select {
	case <-ctx.Done():
		b.closeInbounds()
		_ = b.listener.Close()
		_ = b.upstream.Close()
		if err := <-acceptErrCh; err != nil && ctx.Err() == nil {
			return err
		}
		return nil
	case <-b.upstream.Done():
		b.log.WarnContext(ctx, "mwan-opnsense-host: upstream connection closed",
			"err", b.upstream.Err())
		b.closeInbounds()
		_ = b.listener.Close()
		<-acceptErrCh
		return nil
	case err := <-acceptErrCh:
		b.closeInbounds()
		_ = b.upstream.Close()
		return err
	}
}

func (b *fanInBridge) acceptLoop(ctx context.Context) error {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-b.upstream.Done():
				return nil
			default:
				wrapped := fmt.Errorf("accept inbound: %w", err)
				b.log.ErrorContext(ctx, "mwan-opnsense-host: accept inbound failed",
					slog.Any("err", err))
				return wrapped
			}
		}
		b.addInbound(conn)
	}
}

func (b *fanInBridge) addInbound(rw io.ReadWriteCloser) {
	id := b.nextConnID.Add(1)
	inbound := &inboundConn{
		id:      id,
		bridge:  b,
		pending: make(map[uint64]uint64),
	}
	inbound.conn = mwn1.NewConn(rw, b.log)
	inbound.conn.OnMessage(inbound.handleMessage)

	b.mu.Lock()
	b.inbounds[id] = inbound
	b.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				inbound.bridge.log.Error("mwan-opnsense-host: inbound wait panic recovered",
					slog.Any("err", fmt.Errorf("panic: %v", r)))
			}
		}()
		inbound.wait()
	}()
}

func (i *inboundConn) handleMessage(methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) {
	upstreamCorr := i.upstreamCorr(methodID, corrID)
	if flags&mwn1.FlagStreaming != 0 {
		i.bridge.markRouteStreaming(upstreamCorr)
	}
	if flags&mwn1.FlagCancel != 0 {
		i.bridge.cancelRouteForwarder(upstreamCorr)
		if err := i.bridge.upstream.SendCancel(methodID, upstreamCorr); err != nil &&
			!errors.Is(err, mwn1.ErrClosed) {
			i.bridge.log.Warn("mwan-opnsense-host: forward inbound cancel",
				slog.Uint64("inbound_id", i.id),
				slog.Uint64("inbound_corr", corrID),
				slog.Uint64("upstream_corr", upstreamCorr),
				slog.String("err", err.Error()))
		}
		i.bridge.removeRoute(upstreamCorr)
		return
	}
	if flags&mwn1.FlagStreaming != 0 {
		forwarder := i.bridge.ensureStreamForwarder(upstreamCorr, i)
		forwarder.enqueue(streamForwardJob{
			methodID:     methodID,
			inboundCorr:  corrID,
			upstreamCorr: upstreamCorr,
			flags:        flags,
			payload:      payload,
		})
		return
	}
	if err := i.bridge.upstream.SendMessage(methodID, upstreamCorr, flags, payload); err != nil {
		i.bridge.log.Warn("mwan-opnsense-host: forward inbound frame",
			slog.Uint64("inbound_id", i.id),
			slog.Uint64("inbound_corr", corrID),
			slog.Uint64("upstream_corr", upstreamCorr),
			slog.String("err", err.Error()))
		i.bridge.removeRoute(upstreamCorr)
		_ = i.conn.Close()
	}
}

func (i *inboundConn) upstreamCorr(methodID uint16, inboundCorr uint64) uint64 {
	i.mu.Lock()
	if upstreamCorr, ok := i.pending[inboundCorr]; ok {
		i.mu.Unlock()
		return upstreamCorr
	}
	upstreamCorr := i.bridge.nextUpstreamCorr.Add(1)
	i.pending[inboundCorr] = upstreamCorr
	i.mu.Unlock()

	i.bridge.storeRoute(upstreamCorr, corrRoute{
		inbound:     i,
		inboundCorr: inboundCorr,
		methodID:    methodID,
	})
	return upstreamCorr
}

func (i *inboundConn) wait() {
	<-i.conn.Done()
	i.bridge.removeInbound(i)
}

func (b *fanInBridge) handleUpstreamMessage(methodID uint16, corrID uint64, flags mwn1.Flags, payload []byte) {
	route, ok := b.lookupRoute(corrID)
	if !ok {
		b.log.Warn("mwan-opnsense-host: dropping unmapped upstream frame",
			slog.Uint64("upstream_corr", corrID),
			slog.Uint64("method_id", uint64(methodID)))
		return
	}

	if err := route.inbound.conn.SendMessage(methodID, route.inboundCorr, flags, payload); err != nil {
		b.log.Warn("mwan-opnsense-host: forward upstream frame",
			slog.Uint64("inbound_id", route.inbound.id),
			slog.Uint64("inbound_corr", route.inboundCorr),
			slog.Uint64("upstream_corr", corrID),
			slog.String("err", err.Error()))
		b.removeRoute(corrID)
		return
	}
	if flags&mwn1.FlagStreaming == 0 || flags&mwn1.FlagFinal != 0 {
		b.removeRoute(corrID)
	}
}

func (b *fanInBridge) storeRoute(upstreamCorr uint64, route corrRoute) {
	b.mu.Lock()
	b.routes[upstreamCorr] = route
	b.mu.Unlock()
}

func (b *fanInBridge) lookupRoute(upstreamCorr uint64) (corrRoute, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	route, ok := b.routes[upstreamCorr]
	return route, ok
}

func (b *fanInBridge) removeRoute(upstreamCorr uint64) {
	b.mu.Lock()
	route, ok := b.routes[upstreamCorr]
	if ok {
		delete(b.routes, upstreamCorr)
	}
	b.mu.Unlock()
	if !ok {
		return
	}
	if route.forwarder != nil {
		route.forwarder.cancel()
	}
	route.inbound.mu.Lock()
	delete(route.inbound.pending, route.inboundCorr)
	route.inbound.mu.Unlock()
}

func (b *fanInBridge) removeInbound(inbound *inboundConn) {
	inbound.mu.Lock()
	upstreamCorrs := make([]uint64, 0, len(inbound.pending))
	for _, upstreamCorr := range inbound.pending {
		upstreamCorrs = append(upstreamCorrs, upstreamCorr)
	}
	inbound.pending = make(map[uint64]uint64)
	inbound.mu.Unlock()

	b.mu.Lock()
	delete(b.inbounds, inbound.id)
	cancelRoutes := make([]corrRoute, 0, len(upstreamCorrs))
	cancelCorrs := make([]uint64, 0, len(upstreamCorrs))
	for _, upstreamCorr := range upstreamCorrs {
		if route, ok := b.routes[upstreamCorr]; ok && route.streaming {
			if route.forwarder != nil {
				route.forwarder.cancel()
			}
			cancelRoutes = append(cancelRoutes, route)
			cancelCorrs = append(cancelCorrs, upstreamCorr)
		}
		delete(b.routes, upstreamCorr)
	}
	b.mu.Unlock()
	for index, route := range cancelRoutes {
		upstreamCorr := cancelCorrs[index]
		if err := b.upstream.SendCancel(route.methodID, upstreamCorr); err != nil &&
			!errors.Is(err, mwn1.ErrClosed) {
			b.log.Warn("mwan-opnsense-host: abandoned stream cancel failed",
				slog.Uint64("inbound_id", inbound.id),
				slog.Uint64("upstream_corr", upstreamCorr),
				slog.String("err", err.Error()))
		}
	}
}

func (b *fanInBridge) markRouteStreaming(upstreamCorr uint64) {
	b.mu.Lock()
	route, ok := b.routes[upstreamCorr]
	if ok {
		route.streaming = true
		b.routes[upstreamCorr] = route
	}
	b.mu.Unlock()
}

func (b *fanInBridge) ensureStreamForwarder(upstreamCorr uint64, inbound *inboundConn) *streamForwarder {
	b.mu.Lock()
	route := b.routes[upstreamCorr]
	if route.forwarder != nil {
		forwarder := route.forwarder
		b.mu.Unlock()
		return forwarder
	}
	ctx, cancel := context.WithCancel(context.Background())
	forwarder := &streamForwarder{
		cancel: cancel,
		done:   ctx.Done(),
		jobs:   make(chan streamForwardJob, 1),
	}
	route.streaming = true
	route.forwarder = forwarder
	b.routes[upstreamCorr] = route
	b.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				b.log.Error("mwan-opnsense-host: stream forwarder panic recovered",
					slog.Uint64("inbound_id", inbound.id),
					slog.Uint64("upstream_corr", upstreamCorr),
					slog.Any("err", fmt.Errorf("panic: %v", recovered)))
			}
		}()
		inbound.runStreamForwarder(ctx, forwarder)
	}()
	return forwarder
}

func (b *fanInBridge) cancelRouteForwarder(upstreamCorr uint64) {
	b.mu.Lock()
	route, ok := b.routes[upstreamCorr]
	b.mu.Unlock()
	if ok && route.forwarder != nil {
		route.forwarder.cancel()
	}
}

func (f *streamForwarder) enqueue(job streamForwardJob) {
	select {
	case f.jobs <- job:
	case <-f.done:
	}
}

func (i *inboundConn) runStreamForwarder(ctx context.Context, forwarder *streamForwarder) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-forwarder.jobs:
			err := i.bridge.upstream.SendStreamMessage(
				ctx,
				job.methodID,
				job.upstreamCorr,
				job.flags,
				job.payload,
			)
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, mwn1.ErrClosed) {
					i.bridge.log.WarnContext(ctx, "mwan-opnsense-host: forward inbound stream frame",
						slog.Uint64("inbound_id", i.id),
						slog.Uint64("inbound_corr", job.inboundCorr),
						slog.Uint64("upstream_corr", job.upstreamCorr),
						slog.String("err", err.Error()))
					i.bridge.removeRoute(job.upstreamCorr)
					_ = i.conn.Close()
				}
				return
			}
			if err := i.conn.SendAck(job.methodID, job.inboundCorr); err != nil &&
				!errors.Is(err, mwn1.ErrClosed) {
				i.bridge.log.WarnContext(ctx, "mwan-opnsense-host: ack inbound stream frame",
					slog.Uint64("inbound_id", i.id),
					slog.Uint64("inbound_corr", job.inboundCorr),
					slog.Uint64("upstream_corr", job.upstreamCorr),
					slog.String("err", err.Error()))
				i.bridge.removeRoute(job.upstreamCorr)
				_ = i.conn.Close()
				return
			}
		}
	}
}

func (b *fanInBridge) closeInbounds() {
	b.mu.Lock()
	inbounds := make([]*inboundConn, 0, len(b.inbounds))
	for _, inbound := range b.inbounds {
		inbounds = append(inbounds, inbound)
	}
	b.mu.Unlock()
	for _, inbound := range inbounds {
		_ = inbound.conn.Close()
	}
}
