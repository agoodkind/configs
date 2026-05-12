package main

import (
	"fmt"
	"os"
)

// runOPNsenseHostServe is a stub. The MWN1 proxy was removed when the
// daemon pivoted to gRPC over the virtio-serial Unix socket. Callers
// now dial the qemu socket directly via opnsense.Dial.
func runOPNsenseHostServe(_ []string) int {
	fmt.Fprintln(os.Stderr, "mwan opnsense-host serve: deprecated; dial the qemu virtio-serial socket directly via opnsense.Dial")
	return 2
}
