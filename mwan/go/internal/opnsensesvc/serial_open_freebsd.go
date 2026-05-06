//go:build freebsd

package opnsensesvc

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// OpenVirtioSerial opens a virtio-serial-pci character device for
// read+write in RAW mode. On FreeBSD with virtio_console loaded,
// the named ports appear under /dev/ttyV<bus>.<port>.
//
// Why raw mode: by default, opening a tty character device (which
// /dev/ttyV0.x is, despite being a virtio-serial named port) gives
// you the cooked tty line discipline. That eats arbitrary binary
// bytes, echoes input back, translates newlines, etc. gRPC over
// HTTP/2 over a cooked tty does not work.
//
// We do a cfmakeraw() equivalent: clear all input/output processing,
// echo, signal handling, and canonical mode flags so that bytes flow
// untouched in both directions. Modeled on FreeBSD termios(4) and
// the cfmakeraw(3) BSD documentation.
func OpenVirtioSerial(path string) (io.ReadWriteCloser, error) {
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("OpenVirtioSerial: open %s: %w", path, err)
	}

	t, err := unix.IoctlGetTermios(int(f.Fd()), unix.TIOCGETA)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("OpenVirtioSerial: TIOCGETA %s: %w", path, err)
	}

	// cfmakeraw(3) equivalent.
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8 | unix.CLOCAL
	if unix.HUPCL != 0 {
		t.Cflag &^= unix.HUPCL
	}
	// VMIN=1, VTIME=0 -> blocking read returns as soon as one byte is
	// available, no inter-byte timeout.
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(int(f.Fd()), unix.TIOCSETA, t); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("OpenVirtioSerial: TIOCSETA %s: %w", path, err)
	}

	return f, nil
}
