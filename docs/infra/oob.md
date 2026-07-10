# Emergency out-of-band access

When a host's network is down, an out-of-band path can still reach it, and which path works depends on what is down.

The production OPNsense router has a serial control channel that does not depend on its network stack, so you reach the router even when its network is down. It is described in [the OPNsense out-of-band daemon](../opnsense/daemon.md).

The JetKVM devices, `vault-jetkvm` and `nas-jetkvm`, sit on the Monkeybrains segment and present a keyboard-video-mouse console over IP as an alternate path to those two machines. Their DNS names and credentials are not confirmed, so treat this path as unverified until a live check proves it.

The berylax USB-serial path to the vault console is unavailable while berylax is offline. Its record and its last-known serial procedure are in [berylax.md](berylax.md).
