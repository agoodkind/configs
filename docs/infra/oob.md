# Emergency Out-of-band Access

Berylax is indefinitely offline for now, so the berylax USB-serial OOB path is not
available. Historical berylax serial-console procedure, host notes, and Cloudflare
routing state live in [berylax.md](berylax.md).

JetKVM devices (`vault-jetkvm`, `nas-jetkvm`) are also on the Monkeybrains
segment and may provide an alternate KVM-over-IP console path, though their DNS
names (`vault-jetkvm.goodkind.io`, `nas-jetkvm.goodkind.io`) and credentials are
not confirmed at this time.

The production OPNsense router has its own OOB control channel over qemu virtio-serial,
independent of the network stack. See the [OPNsense OOB daemon](../opnsense/daemon.md).
