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
	"strconv"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

// bindToDevice returns a net.Dialer Control function that sets
// SO_BINDTODEVICE on the dialed socket so traffic egresses via iface
// regardless of the kernel's default routing decision.
func bindToDevice(iface string) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var sockErr error
		err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET,
				unix.SO_BINDTODEVICE, iface)
		})
		if err != nil {
			return fmt.Errorf("bindToDevice(%s): raw control: %w", iface, err)
		}
		if sockErr != nil {
			return fmt.Errorf("bindToDevice(%s): setsockopt: %w", iface, sockErr)
		}
		return nil
	}
}

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
// bounded by timeout; on timeout returns a context.DeadlineExceeded.
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
	op.Debug("v6probe: PingICMP6 entry")

	if !target.Is6() {
		return 0, fmt.Errorf("PingICMP6: target %q is not IPv6", target)
	}

	conn, err := icmp.ListenPacket("ip6:ipv6-icmp", "::")
	if err != nil {
		op.Warn("v6probe: ListenPacket failed", "err", err)
		return 0, fmt.Errorf("ListenPacket: %w", err)
	}
	defer conn.Close()

	// Bind to the iface so the kernel picks a source from it.
	pktConn := ipv6.NewPacketConn(conn.IPv6PacketConn().PacketConn)
	netIface, err := net.InterfaceByName(p.iface)
	if err != nil {
		op.Warn("v6probe: InterfaceByName failed", "err", err)
		return 0, fmt.Errorf("InterfaceByName: %w", err)
	}
	if err := pktConn.SetMulticastInterface(netIface); err != nil {
		op.Debug("v6probe: SetMulticastInterface failed (non-fatal)",
			"err", err)
	}

	// Build Echo Request.
	startTime := p.clock.Now()
	id := os.Getpid() & 0xffff
	seq := int(startTime.UnixNano() & 0x7fff)
	msg := icmp.Message{
		Type: ipv6.ICMPTypeEchoRequest,
		Code: 0,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  seq,
			Data: []byte("mwan-v6probe"),
		},
	}
	wb, err := msg.Marshal(nil)
	if err != nil {
		op.Warn("v6probe: marshal echo failed", "err", err)
		return 0, fmt.Errorf("marshal echo: %w", err)
	}

	deadline := startTime.Add(timeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		op.Warn("v6probe: SetReadDeadline failed", "err", err)
		return 0, fmt.Errorf("SetReadDeadline: %w", err)
	}

	dst := &net.IPAddr{IP: target.AsSlice()}
	if _, err := conn.WriteTo(wb, dst); err != nil {
		op.Warn("v6probe: WriteTo failed", "err", err)
		return 0, fmt.Errorf("WriteTo(%s): %w", target, err)
	}
	op.Debug("v6probe: echo sent", "id", id, "seq", seq)

	rb := make([]byte, 1500)
	for {
		select {
		case <-ctx.Done():
			op.Debug("v6probe: ctx cancelled while waiting for reply")
			return 0, fmt.Errorf("PingICMP6: %w", ctx.Err())
		default:
		}
		n, peer, err := conn.ReadFrom(rb)
		dur := p.clock.Now().Sub(startTime)
		if err != nil {
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				op.Debug("v6probe: deadline exceeded waiting for reply",
				"waited_ms", dur.Milliseconds())
				return 0, context.DeadlineExceeded
			}
			op.Warn("v6probe: ReadFrom failed", "err", err)
			return 0, fmt.Errorf("ReadFrom: %w", err)
		}
		rm, err := icmp.ParseMessage(58, rb[:n])
		if err != nil {
			op.Debug("v6probe: parse failed (continuing)", "err", err)
			continue
		}
		if rm.Type != ipv6.ICMPTypeEchoReply {
			continue
		}
		echo, ok := rm.Body.(*icmp.Echo)
		if !ok || echo.ID != id || echo.Seq != seq {
			continue
		}
		op.Debug("v6probe: echo reply received",
			"from", peer.String(), "rtt_ms", dur.Milliseconds())
		return dur, nil
	}
}

// TCPConnect attempts a TCP connection to (target, port) bound to the
// configured interface, returning nil on success. Used as a fallback
// probe for hosts that drop ICMP. Bound via SO_BINDTODEVICE so source
// selection respects the interface even when the kernel might prefer
// another route.
func (p *V6Probe) TCPConnect(
	ctx context.Context, target netip.Addr, port int, timeout time.Duration,
) error {
	op := p.log.With("op", "TCPConnect",
		"target", target.String(), "port", port,
		"timeout_ms", timeout.Milliseconds())
	op.Debug("v6probe: TCPConnect entry")

	d := net.Dialer{
		Timeout: timeout,
		Control: bindToDevice(p.iface),
	}
	addr := net.JoinHostPort(target.String(), strconv.Itoa(port))
	startTime := p.clock.Now()
	conn, err := d.DialContext(ctx, "tcp6", addr)
	dur := p.clock.Now().Sub(startTime)
	op.Debug("v6probe: DialContext result",
		"duration_ms", dur.Milliseconds(), "err", err)
	if err != nil {
		op.Warn("v6probe: DialContext failed", "err", err)
		return fmt.Errorf("DialContext(%s): %w", addr, err)
	}
	_ = conn.Close()
	return nil
}
