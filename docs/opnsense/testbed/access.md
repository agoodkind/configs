# Reaching the testbed OPNsense guest

The testbed OPNsense guest carries a virtio-serial channel that works when its
network does not. Reach it with `mwan opnsense exec` and `mwan opnsense config`
from the suburban hypervisor. The channel does not depend on the guest's
addresses, its packet filter, or its routing, so it survives a renumber, a
filter that drops your source, and a rebuild that changes the host key.

Use it whenever SSH fails. A connection that times out means the packet filter
dropped it, and a rebuild changes the host key, so both send you here. Use the
config-import gate in [import.md](import.md) for a rebuild, and
[dns.md](dns.md) when a rebuilt guest comes up with name resolution broken.

## Verify the channel

Confirm the guest-side virtio-console symlink before assuming the serial device:

```sh
ls -l /dev/vtcon/io.goodkind.mwan-opnsense.0
```

If the symlink points somewhere other than the configured device, update
`mwan_opnsense_listen_serial` before starting `mwan_opnsense`.

Confirm the host-side path from suburban. The socket is named for the guest's
id, so read it from the rendered config rather than typing it:

```sh
ssh suburban 'mwan opnsense version -target "$(sed -n "s/^chardev = \"\(.*\)\"/\1/p" /etc/mwan/config.toml)"'
```

The command returns the daemon build banner.

## Why suburban reaches the guests through the router

Suburban holds no address on the transit bridge, matching vault, and routes to
that segment through the guest segment instead. Production's vault sits on the
transit bridge and reaches its failover across it directly, so this extra hop is
the one sanctioned divergence between the two environments. It exists because
the testbed router is destroyed and rebuilt often, and suburban must keep
reaching the guests across those rebuilds.

The hop depends on a firewall rule passing guest-segment traffic to the transit
networks, which the config transform inserts. Without that rule, suburban and
the workstation reach neither the MWAN VM nor the failover, and both fail as a
timeout rather than a refusal.
