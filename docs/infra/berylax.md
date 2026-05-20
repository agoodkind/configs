# Berylax

## Current status

Berylax is indefinitely offline for now. Treat the berylax USB-serial OOB path,
the `home-berylax` Cloudflare tunnel, and the `3d06:bad:b01:300::/64` WARP route
as unavailable until a fresh live check says otherwise.

Broader infrastructure state lives in [overview.md](overview.md).

## Historical host notes

Berylax was an OpenWrt 24.10.5 GL.iNet device on the Monkeybrains L2 segment.
Its `eth0` interface used Monkeybrains IPv4 and provider SLAAC on the WAN /64.
Its `br-lan` interface used `3d06:bad:b01:300::1/64` as a static fake GUA LAN
address.

Berylax used msmtp with SMTP2GO through `/usr/local/bin/send-email` and
`/usr/local/bin/send-email-smtp2go`. It was not Ansible-managed.

The last recorded LAN state used fake GUA `3d06:bad:b01:300::/64` with NPT to
the WAN SLAAC /64 on `eth0` and no Monkeybrains PD. `ndppd` proxied WAN-delegated
traffic on `eth0`; downstream `3d06:bad:b01:300::100` IPv6 tests received
replies. The NPT prerouting rule exempted local WAN addresses with
`fib daddr . iif type local accept`, so the router's own WAN address stayed
reachable. Inbound IPv4 and IPv6 ping and SSH were verified on 2026-03-28 from
CT 116, and inbound IPv6 ping plus SSH were also verified from a local Mac.
Laptops reached `3d06:bad:b01:300::1` via the Cloudflare WARP route on tunnel
`home-berylax`, not via OPNsense. No WireGuard was installed.

## Historical OOB serial path

The berylax OOB serial path is unavailable while berylax is offline. These notes
record the last known procedure.

When vault's IPv6 network is unreachable, the berylax USB-serial adapter was the
only in-band path to vault's console.

**Prerequisites:**

- berylax was on the Monkeybrains L2 segment, the same physical switch as
  vault's server. It was not on the `3d06:bad:b01::/48` management network, so
  it could not SSH to vault directly.
- A USB-to-serial cable ran from berylax (`/dev/ttyUSB0`) to vault's physical
  serial port.
- vault had a serial console configured at 115200 8N1. Proxmox's GRUB and the
  Linux kernel both output to this port.

**Preferred tool: `serial-exec`** ([github.com/agoodkind/serial-exec](https://github.com/agoodkind/serial-exec)).
Rust CLI that runs on berylax (static arm64 musl binary, no dependencies). Uses a
sentinel-based protocol for reliable output capture and exit code extraction over serial.

```bash
ssh berylax '/tmp/serial-exec run vault "qm list" --json'
ssh berylax '/tmp/serial-exec shell vault'
ssh berylax '/tmp/serial-exec ping vault'
```

Config on berylax: `~/.config/serial-exec/hosts.toml`

```toml
[hosts.vault]
device = "/dev/ttyUSB0"
baud = 115200
prompt = '(?m)[#$] $'
user = "root"
```

If `serial-exec` is unavailable, fall back to `screen /dev/ttyUSB0 115200` on berylax.

### Procedure: run commands on vault via serial console

```bash
# 1. SSH into berylax
ssh berylax

# 2. Start a detached screen session logging serial output to a file
screen -dmS vault-serial /dev/ttyUSB0 115200
sleep 1
screen -S vault-serial -X logfile /tmp/vault-serial.log
screen -S vault-serial -X log on

# 3. Send a command, for example press Enter to get a prompt, then run a command
screen -S vault-serial -X stuff $'\r'
sleep 1
screen -S vault-serial -X stuff "qm status 113\r"
sleep 3

# 4. Read the output
cat /tmp/vault-serial.log

# 5. To start a stopped VM:
screen -S vault-serial -X stuff "qm start 113\r"
sleep 15
cat /tmp/vault-serial.log
```

**Notes:**

- `screen` uses `-X stuff` to send keystrokes to the serial TTY. The `\r` at the
  end of each command string is the carriage return.
- Output contains ANSI escape codes for color and cursor positioning. The log is
  still readable but has control sequences mixed in.
- vault's zsh prompt looked like `vault ~ >` in the serial output. If the screen
  was blank, pressing Enter once woke it.
- picocom and minicom were also available on berylax, but both required a real
  PTY for interactive use. Screen's `stuff` approach worked non-interactively
  from a script or SSH session.
- The serial session persisted as long as berylax was up. `screen -ls` checked
  whether one was already running.
- If vault was mid-boot, kernel messages scrolled through. The operator waited
  for the login prompt or zsh prompt before sending commands.

**What this covered:**

- MWAN VM 113 stopped and network down: start it with `qm start 113` via serial.
- Vault SSH unreachable but host up: run arbitrary commands via serial.
- Vault BIOS and GRUB: visible on serial at boot at 115200 baud.

**What this did not cover:**

- vault fully powered off or kernel-panicked: requires physical access or IPMI/iDRAC.
- berylax unreachable: no remote OOB path available; physical access to the rack
  is required.
- JetKVM devices (`vault-jetkvm`, `nas-jetkvm`) are also on the Monkeybrains
  segment and may provide an alternate KVM-over-IP console path, though their DNS
  names (`vault-jetkvm.goodkind.io`, `nas-jetkvm.goodkind.io`) and credentials
  were not confirmed at the last recorded check.
