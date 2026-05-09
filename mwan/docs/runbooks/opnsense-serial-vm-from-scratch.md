# OPNsense Serial VM From Scratch

Created: 2026-05-07

This runbook describes the agent-safe path for creating a fresh OPNsense VM on
the suburban Proxmox testbed and bringing it to a basic operational state with
SSH, QEMU Guest Agent, and MWN1 access.

It was first practiced on VM `129` and then rehearsed end-to-end on VM `130`.
Do not run it against production or the existing testbed router VM `101`.

## Safety Boundary

- Target hypervisor: suburban only, `root@10.240.0.148`.
- Do not touch production.
- Do not mutate existing suburban VM `101`, VM `950`, LXC `100`, or ISP LXCs
  `200`, `201`, or `202`.
- Use an unused VMID and a unique LAN IPv4 address on `10.240.200.0/24`.
- Before each destructive command, verify the command references only the chosen
  practice VMID and its disks.
- If an installer prompt is ambiguous, stop instead of guessing.

## Validated Inputs

- Installer image on suburban:
  `/var/lib/vz/template/iso/OPNsense-25.7-serial-amd64.img`
- Hypervisor storage: `local-zfs`.
- Network bridge for practice management: `vmbr1`.
- Serial console: Proxmox `serial0: socket` and `vga: serial0`.
- MWN1 virtio-serial name: `io.goodkind.mwan-opnsense.0`.

## Preflight

Choose a new VMID and address. The rehearsal used:

```text
VMID=130
NAME=opnsense-serial-rehearsal
LAN_IPV4=10.240.200.130
```

Verify the ID is unused and protected IDs are only read:

```bash
ssh root@10.240.0.148 'qm list; pct list; qm config 130 2>&1 || true'
```

Proceed only if the chosen VMID has no existing config.

## Create VM

Create the VM with serial console, QGA enabled, one management NIC, and a
separate MWN1 virtio-serial channel:

```bash
ssh root@10.240.0.148 'qm create 130 \
    --name opnsense-serial-rehearsal \
    --memory 4096 \
    --cores 2 \
    --ostype other \
    --scsihw virtio-scsi-pci \
    --serial0 socket \
    --vga serial0 \
    --net0 virtio,bridge=vmbr1 \
    --agent enabled=1 \
    --args '"'"'-device virtio-serial-pci,id=mwanrpc -chardev socket,id=mwanchr,path=/var/run/qemu-server/130.mwanrpc,server=on,wait=off -device virtserialport,chardev=mwanchr,name=io.goodkind.mwan-opnsense.0'"'"''
```

Attach the serial installer image as `scsi0`, create a 16G install target as
`scsi1`, and boot the installer:

```bash
ssh root@10.240.0.148 'qm set 130 --scsi0 local-zfs:0,import-from=/var/lib/vz/template/iso/OPNsense-25.7-serial-amd64.img'
ssh root@10.240.0.148 'qm set 130 --scsi1 local-zfs:16,discard=on'
ssh root@10.240.0.148 'qm set 130 --boot order=scsi0'
ssh root@10.240.0.148 'qm start 130'
```

The earlier 8G target disk failed during ZFS install with:

```text
gpart: autofill: No space left on device
```

Use 16G or larger.

## Serial Console

Use a TTY. Non-TTY `qm terminal` fails with `tcgetattr`.

```bash
ssh -tt root@10.240.0.148 'qm terminal 130'
```

The live image may pause at:

```text
Press any key to start the configuration importer:
```

Waiting can continue safely. The live image may also auto-run default interface
assignment and assign `LAN -> vtnet0`.

Wait for:

```text
FreeBSD/amd64 (OPNsense.internal) (ttyu0)
login:
```

Live image credentials:

```text
login: root
Password: opnsense
```

Use menu option `8` for shell when needed.

## Install OPNsense

Log in as the installer:

```text
login: installer
Password: opnsense
```

Validated installer path:

```text
Keymap Selection: Continue with default keymap
OPNsense Installer: Install (ZFS)
ZFS Configuration: stripe
Disk selection: select da1 QEMU QEMU HARDDISK with Space
Disk selection: press Enter directly after Space on da1
Last Chance: verify the prompt lists only da1, then Tab to YES and Enter
Final Configuration: press c for Complete Install, then Enter
Installation Complete: press h for Halt now, then Enter
```

Important: in the VM `130` rehearsal, pressing `Tab` after selecting `da1`
moved focus to `Back`. Press `Enter` directly after pressing `Space` on `da1`.

After the VM halts, boot the installed disk:

```bash
ssh root@10.240.0.148 'qm set 130 --boot order=scsi1'
ssh root@10.240.0.148 'qm start 130'
```

Installed boot should show:

```text
Root file system: zroot/ROOT/default
```

## First Boot Network Setup

Use console menu option `2` to configure LAN:

```text
Configure IPv4 address LAN interface via DHCP? n
Enter the new LAN IPv4 address: 10.240.200.130
Enter the new LAN IPv4 subnet bit count: 24
For a LAN, press ENTER for no upstream gateway
Configure IPv6 address LAN interface via DHCP6? n
Enter the new LAN IPv6 address: ENTER
Enable DHCP server on LAN? n
Change web GUI protocol from HTTPS to HTTP? n
Generate a new self-signed web GUI certificate? n
Restore web GUI access defaults? y
```

Expected result:

```text
https://10.240.200.130
LAN (vtnet0) -> v4: 10.240.200.130/24
```

## Enable SSH

From shell option `8`, start `/bin/sh` first because the root shell is `csh`:

```sh
sh
sysrc sshd_enable=NO
sysrc openssh_enable=YES
sysrc openssh_skipportscheck=YES
service openssh start
```

Validate from suburban:

```bash
ssh root@10.240.0.148 'nc -vz -w 3 10.240.200.130 22'
ssh root@10.240.0.148 'sshpass -p opnsense ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/mwan130_known_hosts root@10.240.200.130 "echo ssh-ok"'
```

## Enable QEMU Guest Agent

Inside OPNsense:

```sh
route add default 10.240.200.1 || true
printf 'nameserver 1.1.1.1\n' >/etc/resolv.conf
pkg update -f
pkg upgrade -y
pkg install -y os-qemu-guest-agent
sysrc qemu_guest_agent_enable=YES
service qemu-guest-agent start
```

The first package operation may upgrade `pkg` before installing
`os-qemu-guest-agent`.

The `pkg upgrade -y` step is required. The OPNsense ISO ships a baseline
package set (frozen at release time), and the package mirror ships a current
set. Mixing the two without a full upgrade produces a hybrid set with
potential ABI skew. Concrete example observed on 2026-05-08: prod VM 101
carries `pcre2-10.47_1` and `libyang2-2.1.128`, while a VM 102 baseline
built without `pkg upgrade -y` retained the ISO's `pcre2-10.45_1` alongside
`libyang2-2.1.128` from the mirror; `libyang.so.2` requires the `PCRE2_10.47`
symbol set and `vtysh` failed at startup with a dynamic linker error until
the package set was reconciled. Running `pkg upgrade -y` before installing
`os-qemu-guest-agent` and `os-frr` keeps the package set self-consistent.

Validate from suburban:

```bash
ssh root@10.240.0.148 'qm guest cmd 130 ping'
ssh root@10.240.0.148 'qm guest exec 130 -- /bin/hostname'
```

The hostname should be:

```text
OPNsense.internal
```

## Generate API Key For Root

OPNsense API access requires an API key/secret pair tied to a user. The
`createKey` method on `OPNsense\Auth\API` generates the pair and persists it
to the user's apikeys list. Run this on the VM via the gRPC exec channel or
serial console; do not echo the resulting key or secret to logs.

Create the helper script on the VM:

```bash
cat > /tmp/create_api_key.php <<'PHP'
<?php
chdir('/usr/local/opnsense/mvc');
require_once('script/load_phalcon.php');
use OPNsense\Auth\API;
$api = new API();
$result = $api->createKey('root');
if ($result === false) { fwrite(STDERR, "createKey returned false\n"); exit(1); }
echo $result['key'] . "\0" . $result['secret'];
PHP
```

Run it:

```bash
php /tmp/create_api_key.php
```

Stdout is `<key>\0<secret>` (null-byte separated). The operator splits the
output into two mode-600 tempfiles on the host, never echoing either value to
the chat or persistent logs.

Verify the credential pair against the firmware status endpoint:

```bash
curl -k -u "$KEY:$SECRET" https://<lan_ip>/api/core/firmware/status
```

A `200` response confirms the key is active.

## Install MWN1 Daemon

Build artifacts from `mwan/go`:

```bash
go test ./cmd/mwan ./internal/opnsensesvc ./internal/opnsense ./internal/mwn1
make -o check build-mwan-opnsense
make -o check build-linux
```

Copy these files to a temporary directory on suburban:

- `bin/mwan-opnsense`
- `bin/mwan-linux`
- `cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense`
- `cmd/mwan/opnsense-src/etc/rc.conf.d/mwan_opnsense.sample`
- `cmd/mwan/opnsense-src/boot/loader.conf.d/mwan_opnsense.conf`

Then copy into OPNsense. The rehearsal used password SSH from suburban after SSH
was validated:

```bash
ssh root@10.240.0.148 'sshpass -p opnsense scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/mwan130_known_hosts /tmp/mwan130-practice/mwan-opnsense root@10.240.200.130:/usr/local/sbin/mwan-opnsense.current'
ssh root@10.240.0.148 'sshpass -p opnsense scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/mwan130_known_hosts /tmp/mwan130-practice/etc-rc.d/mwan_opnsense root@10.240.200.130:/usr/local/etc/rc.d/mwan_opnsense'
ssh root@10.240.0.148 'sshpass -p opnsense scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/mwan130_known_hosts /tmp/mwan130-practice/etc-rc.conf.d/mwan_opnsense root@10.240.200.130:/etc/rc.conf.d/mwan_opnsense'
ssh root@10.240.0.148 'sshpass -p opnsense scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/mwan130_known_hosts /tmp/mwan130-practice/loader-conf.d/mwan_opnsense.conf root@10.240.200.130:/boot/loader.conf.d/mwan_opnsense.conf'
```

Inside OPNsense:

```sh
chmod 0555 /usr/local/sbin/mwan-opnsense.current /usr/local/etc/rc.d/mwan_opnsense
ln -sf /usr/local/sbin/mwan-opnsense.current /usr/local/sbin/mwan-opnsense
sysrc mwan_opnsense_enable=YES
service mwan_opnsense start
service mwan_opnsense status
```

Expected status:

```text
mwan-opnsense is running
```

Expected virtio console mapping:

```text
/dev/vtcon/io.goodkind.mwan-opnsense.0 -> ../ttyV0.1
/dev/vtcon/org.qemu.guest_agent.0 -> ../ttyV1.1
```

## Host-Side Bridge And Probe

Run a temporary bridge on suburban:

```bash
ssh -tt root@10.240.0.148 'rm -f /tmp/mwan130-opnsense.sock; /tmp/mwan130-practice/mwan-linux opnsense-host serve -upstream unix:///var/run/qemu-server/130.mwanrpc -listen /tmp/mwan130-opnsense.sock'
```

Probe from another suburban shell:

```bash
ssh root@10.240.0.148 '/tmp/mwan130-practice/mwan-linux opnsense-probe -target unix:///tmp/mwan130-opnsense.sock -op version'
ssh root@10.240.0.148 '/tmp/mwan130-practice/mwan-linux opnsense-probe -target unix:///tmp/mwan130-opnsense.sock -op exec -cmd /bin/hostname'
```

Expected results:

```text
opnsense-probe Version OK
OPNsense.internal
```

## Final Reboot Validation

Reboot the VM:

```bash
ssh root@10.240.0.148 'qm reboot 130'
```

SSH and QGA may be briefly unavailable immediately after reboot even when the
serial console already shows the login prompt. Wait and validate:

```bash
ssh root@10.240.0.148 'nc -vz -w 3 10.240.200.130 22'
ssh root@10.240.0.148 'qm guest cmd 130 ping'
ssh root@10.240.0.148 'qm guest exec 130 -- /bin/hostname'
ssh root@10.240.0.148 'sshpass -p opnsense ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/tmp/mwan130_known_hosts root@10.240.200.130 "service qemu-guest-agent status; service openssh status; service mwan_opnsense status"'
```

Restart the temporary bridge if needed and re-run:

```bash
ssh root@10.240.0.148 '/tmp/mwan130-practice/mwan-linux opnsense-probe -target unix:///tmp/mwan130-opnsense.sock -op version'
ssh root@10.240.0.148 '/tmp/mwan130-practice/mwan-linux opnsense-probe -target unix:///tmp/mwan130-opnsense.sock -op exec -cmd /bin/hostname'
```

## Validated Rehearsal

The VM `130` rehearsal completed on 2026-05-07.

Final validation passed:

- SSH reachable at `10.240.200.130:22`.
- `qm guest cmd 130 ping` exited `0`.
- `qm guest exec 130 -- /bin/hostname` returned `OPNsense.internal`.
- Guest services reported `qemu_guest_agent`, `openssh`, and `mwan-opnsense`
  running.
- Host bridge probes returned `version` and `OPNsense.internal`.

The only runbook correction from the rehearsal was the disk-selection behavior:
after selecting `da1`, press `Enter` directly. Do not `Tab` first.
