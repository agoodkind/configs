//go:build freebsd

package opnsensesvc

import (
	"fmt"
	"io"
	"os"
)

// OpenVirtioSerial opens a virtio-serial-pci character device for
// read+write. On FreeBSD with virtio_console loaded, the named ports
// appear under /dev/ttyV<bus>.<port>.
func OpenVirtioSerial(path string) (io.ReadWriteCloser, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("OpenVirtioSerial: %s: %w", path, err)
	}
	return f, nil
}
