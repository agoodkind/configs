//go:build linux

package opnsensesvc

import (
	"fmt"
	"io"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSerialListener_LinuxPTYReconnect(t *testing.T) {
	master, slavePath := openTestPTY(t)
	defer func() { _ = master.Close() }()

	listener := newTestListener(slavePath, openRawTestTTY)
	defer func() { _ = listener.Close() }()

	conn1, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := master.WriteString("one"); err != nil {
		t.Fatal(err)
	}
	assertReadString(t, conn1, "one")
	if err := conn1.Close(); err != nil {
		t.Fatal(err)
	}

	conn2, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := master.WriteString("two"); err != nil {
		t.Fatal(err)
	}
	assertReadString(t, conn2, "two")
	if err := conn2.Close(); err != nil {
		t.Fatal(err)
	}
}

func openTestPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	ptyNumber, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	return master, fmt.Sprintf("/dev/pts/%d", ptyNumber)
}

func openRawTestTTY(path string) (io.ReadWriteCloser, error) {
	file, err := os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	termios, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	termios.Oflag &^= unix.OPOST
	termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	termios.Cflag &^= unix.CSIZE | unix.PARENB
	termios.Cflag |= unix.CS8
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, termios); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func assertReadString(t *testing.T, reader io.Reader, want string) {
	t.Helper()
	buffer := make([]byte, len(want))
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatal(err)
	}
	if got := string(buffer); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
