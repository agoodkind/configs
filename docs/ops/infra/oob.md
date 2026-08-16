# Emergency out-of-band access

When a host's network is down, an out-of-band path can still reach it, and which path works depends on what is down.

The production OPNsense router has a serial control channel that does not depend on its network stack, so you reach the router even when its network is down. It is described in [the OPNsense out-of-band daemon](../opnsense/daemon.md).

The vault hypervisor has no serial out-of-band path. If vault itself is off the network, recovery runs through its own Cloudflare tunnel while the host is up, and through physical access when it is not.
