//go:build !linux

package main

import (
	"fmt"
	"os"
)

// runDeployGate is Linux-only: it probes egress with raw ICMP sockets and
// reads the guest boot_id through the Proxmox qm CLI, both of which exist
// only on the hypervisor.
func runDeployGate(_ []string) int {
	fmt.Fprintln(os.Stderr, "mwan deploy-gate requires linux")
	return 1
}
