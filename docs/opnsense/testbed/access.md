# Recover testbed OPNsense without network access

Use the serial channel when the testbed router's network or SSH daemon is
unavailable. The channel connects suburban to the guest without using the
guest's addresses, packet filter, or routing.

Open a direct SSH session to suburban using the
[infrastructure access guide](../../infra/access.md). Run the commands below
from that host.

## Verify the channel

```sh
mwan opnsense daemon version
mwan opnsense exec /bin/hostname
```

The first command returns the running daemon's build identity. The second
returns the guest hostname. Both commands must succeed before recovery work.

## Run a recovery command

Run a guest command through the serial channel:

```sh
mwan opnsense exec <command> [args...]
```

Inspect the current configuration commands before changing the router:

```sh
mwan opnsense config --help
```

The [OPNsense serial daemon](../daemon.md) defines the channel's operating and
recovery limits.

## Escalate when the channel is unavailable

Use the serial-console path in the
[infrastructure access guide](../../infra/access.md). Repair the guest endpoint
with the [OPNsense installation guide](../install.md).
