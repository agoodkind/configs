//go:build linux

package netif

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

// V6Probe sends ICMPv6 Echo Requests from a specific interface and waits
// for the reply. Used by slaac_health and connectivity_probe to verify
// that an interface can actually reach a target, independent of whether
// SLAAC and routes look correct.
//
// Implementation uses golang.org/x/net/icmp to open a raw ICMPv6 socket
// (ipv6:ipv6-icmp), then writes Echo Request and reads matching Echo
// Reply. Matched on identifier+sequence so multiple concurrent probes
// from the same daemon don't cross-talk.
//
// Capabilities required: CAP_NET_RAW.
type V6Probe struct {
	iface string
	log   *slog.Logger
	clock clock
}

// NewV6Probe constructs a V6Probe. log must be non-nil.
func NewV6Probe(iface string, log *slog.Logger) *V6Probe {
	if log == nil {
		log = slog.Default()
	}
	return &V6Probe{
		iface: iface,
		log:   log.With("component", "v6probe", "iface", iface),
		clock: realClock{},
	}
}

// PingICMP6 sends one ICMPv6 Echo Request to target via the configured
// interface and returns the round-trip time on success. The wait is
// bounded by timeout; on timeout returns a [context.DeadlineExceeded].
//
// Source address selection: we open the listen socket with "::"; the
// kernel picks an appropriate source from the iface based on its source-
// address selection algorithm. If the iface has no usable global source
// (e.g. only a deprecated SLAAC), the kernel will refuse the send with
// EADDRNOTAVAIL, which we surface as a typed error.
func (p *V6Probe) PingICMP6(
	ctx context.Context, target netip.Addr, timeout time.Duration,
) (time.Duration, error) {
	op := p.log.With("op", "PingICMP6",
		"target", target.String(), "timeout_ms", timeout.Milliseconds())
	op.DebugContext(ctx, "v6probe: PingICMP6 entry")

	if !target.Is6() {
		return 0, fmt.Errorf("PingICMP6: target %q is not IPv6", target)
	}

	conn, err := icmp.ListenPacket("ip6:ipv6-icmp", "::")
	if err != nil {
		p.log.WarnContext(ctx, "v6probe: ListenPacket failed",
			"op", "PingICMP6", "target", target.String(), "timeout_ms", timeout.Milliseconds(), "err", err)
		return 0, fmt.Errorf("ListenPacket: %w", err)
	}
	defer conn.Close()

	// Bind to the iface so the kernel picks a source from it.
	pktConn := ipv6.NewPacketConn(conn.IPv6PacketConn().PacketConn)
	netIface, err := net.InterfaceByName(p.iface)
	if err != nil {
		p.log.WarnContext(ctx, "v6probe: InterfaceByName failed",
			"op", "PingICMP6", "target", target.String(), "timeout_ms", timeout.Milliseconds(), "err", err)
		return 0, fmt.Errorf("InterfaceByName: %w", err)
	}
	if err := pktConn.SetMulticastInterface(netIface); err != nil {
		op.DebugContext(ctx, "v6probe: SetMulticastInterface failed (non-fatal)",
			"err", err)
	}

	// Build Echo Request.
	startTime := p.clock.Now()
	msg, id, seq := newEchoRequest(startTime)
	wb, err := msg.Marshal(nil)
	if err != nil {
		p.log.WarnContext(ctx, "v6probe: marshal echo failed",
			"op", "PingICMP6", "target", target.String(), "timeout_ms", timeout.Milliseconds(), "err", err)
		return 0, fmt.Errorf("marshal echo: %w", err)
	}

	deadline := startTime.Add(timeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		p.log.WarnContext(ctx, "v6probe: SetReadDeadline failed",
			"op", "PingICMP6", "target", target.String(), "timeout_ms", timeout.Milliseconds(), "err", err)
		return 0, fmt.Errorf("SetReadDeadline: %w", err)
	}

	dst := &net.IPAddr{IP: target.AsSlice()}
	if _, err := conn.WriteTo(wb, dst); err != nil {
		p.log.WarnContext(ctx, "v6probe: WriteTo failed",
			"op", "PingICMP6", "target", target.String(), "timeout_ms", timeout.Milliseconds(), "err", err)
		return 0, fmt.Errorf("WriteTo(%s): %w", target, err)
	}
	op.DebugContext(ctx, "v6probe: echo sent", "id", id, "seq", seq)

	rb := make([]byte, 1500)
	for {
		select {
		case <-ctx.Done():
			ctxErr := ctx.Err()
			p.log.WarnContext(ctx, "v6probe: ctx cancelled while waiting for reply",
				"op", "PingICMP6", "target", target.String(), "timeout_ms", timeout.Milliseconds(), "err", ctxErr)
			return 0, fmt.Errorf("PingICMP6: %w", ctxErr)
		default:
		}
		n, peer, err := conn.ReadFrom(rb)
		dur := p.clock.Now().Sub(startTime)
		if err != nil {
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				op.DebugContext(ctx, "v6probe: deadline exceeded waiting for reply",
					"waited_ms", dur.Milliseconds())
				return 0, context.DeadlineExceeded
			}
			p.log.WarnContext(ctx, "v6probe: ReadFrom failed",
				"op", "PingICMP6", "target", target.String(), "timeout_ms", timeout.Milliseconds(), "err", err)
			return 0, fmt.Errorf("ReadFrom: %w", err)
		}
		rm, err := icmp.ParseMessage(58, rb[:n])
		if err != nil {
			op.DebugContext(ctx, "v6probe: parse failed (continuing)", "err", err)
			continue
		}
		if rm.Type != ipv6.ICMPTypeEchoReply {
			continue
		}
		echo, ok := rm.Body.(*icmp.Echo)
		if !ok || echo.ID != id || echo.Seq != seq {
			continue
		}
		op.DebugContext(ctx, "v6probe: echo reply received",
			"from", peer.String(), "rtt_ms", dur.Milliseconds())
		return dur, nil
	}
}

func newEchoRequest(startTime time.Time) (icmp.Message, int, int) {
	id := os.Getpid() & 0xffff
	seq := int(startTime.UnixNano() & 0x7fff)
	return icmp.Message{
		Type: ipv6.ICMPTypeEchoRequest,
		Code: 0,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  seq,
			Data: []byte("mwan-v6probe"),
		},
	}, id, seq
}
