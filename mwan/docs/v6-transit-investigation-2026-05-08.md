# V6 transit to suburban (3d06:bad:b01:200::1) failure on 2026-05-08

## Symptom

V6 transit between this Mac and `3d06:bad:b01:200::1` (suburban hypervisor v6
address) is failing. V4 transit on `10.240.0.148:8006` continues to work, with
elevated latency. The same v6 address is also unreachable from vault, and
suburban's own cloudflared instance is hitting i/o timeouts dialing
`[3d06:bad:b01:200::1]:8006`.

## Probable root cause

The Tofu apply that created `vmbrtrunk` on suburban tonight rewrote
`/etc/network/interfaces` via the PVE API and stripped
`address 3d06:bad:b01:200::1/64` off the `vmbr1` stanza. The address is now
absent from every interface on suburban, so nothing answers for it.

The PVE Proxmox Tofu provider rewrites the entire bridge stanza using only the
fields it manages. The v6 address on vmbr1 was never imported into the Tofu
state for `proxmox_network_linux_bridge.vmbr1` (or whichever resource owns
that bridge), so when the apply also touched `/etc/network/interfaces` to add
`vmbrtrunk` and reload networking, vmbr1 came back without the v6 address.

## Evidence

### 1. Mac to suburban ping fails

```
$ ping6 -c 3 3d06:bad:b01:200::1
PING6(56=40+8+8 bytes) 2606:4700:cf1:1000::5 --> 3d06:bad:b01:200::1
3 packets transmitted, 0 packets received, 100.0% packet loss
```

### 2. Vault to suburban ping fails too

```
$ ssh vault 'ping6 -c 3 -W 2 3d06:bad:b01:200::1'
3 packets transmitted, 0 received, 100% packet loss
$ ssh vault 'ip -6 route get 3d06:bad:b01:200::1'
3d06:bad:b01:200::1 from :: via 3d06:bad:b01::1 dev vmbr0 ... pref medium
```

### 3. Suburban itself has no global v6 address on vmbr1

```
$ ip -6 addr show vmbr1
5: vmbr1: <BROADCAST,MULTICAST,UP,LOWER_UP> ...
    inet6 fe80::1/64 scope link
    inet6 fe80::cc14:50ff:fe55:885d/64 scope link proto kernel_ll
```

Searching across all interfaces:

```
$ ip -6 addr show | grep -E "200::|3d06:bad:b01:200"
no global 200:: address found anywhere
```

`ip -6 route get 3d06:bad:b01:200::1` from suburban falls through to the
upstream default route on vmbr0 (Comcast) because no connected route covers
the prefix.

### 4. Current /etc/network/interfaces does not have the address

```
$ ssh suburban 'grep -n "200::1" /etc/network/interfaces'
(no match)
$ ssh suburban 'grep -rE "200::1|3d06:bad:b01:200" /etc/network/'
/etc/network/interfaces.bak.20260328-211438:    address 3d06:bad:b01:200::1/64
/etc/network/interfaces.bak.20260328-211154:    address 3d06:bad:b01:200::1/64
... (older backups also have it)
```

The most recent backup `/etc/network/interfaces.bak-20260425-234300` (Apr 25)
still had the address. The current file (mtime `May 8 00:13`) does not.

### 5. The diff between Apr 25 backup and current file is a clean removal

```
$ diff /etc/network/interfaces.bak-20260425-234300 /etc/network/interfaces
40d39
<       address 3d06:bad:b01:200::1/64
... (plus vmbrtrunk added at the bottom)
```

### 6. The PVE network reload that did this is in the syslog

```
2026-05-08T00:13:37.765951-07:00 pvedaemon[1730959]: <root@pam!watchdog-test2>
    update VM 101: -agent enabled=1,fstrim_cloned_disks=0 ...
2026-05-08T00:13:38.324555-07:00 pvedaemon[1730959]: <root@pam!watchdog-test2>
    starting task UPID:hypervisor:002F094A:1031FA18:69FD8D22:
    srvreload:networking:root@pam!watchdog-test2:
2026-05-08T00:13:38.695021-07:00 kernel: vmbr0: the hash_elasticity option
    has been deprecated and is always 16
... (same for vmbr1 .. vmbr6, then vmbrtrunk creation)
2026-05-08T00:13:38.920267-07:00 pvedaemon[1730959]: <root@pam!watchdog-test2>
    end task ... srvreload:networking:root@pam!watchdog-test2: OK
```

The PVE token `root@pam!watchdog-test2` is the token used by the Tofu Proxmox
provider in `terraform.tfvars`. The `srvreload:networking` action is what
reloads `/etc/network/interfaces`.

### 7. mwan watchdog observed the address removal and the new bridge in the same diff

```
2026-05-08T00:13:57.421592 mwan watchdog WARN: vault host interface address changed
diff:
  REMOVED  vmbr1     3d06:bad:b01:200::1/64
  ADDED    vmbrtrunk fe80::c874:56ff:fec6:247e/64 (new interface)
```

This proves the address was removed at the same instant the bridge was added,
confirming both events came from the single Tofu API call.

### 8. Cloudflared on suburban shows the loss in real time

Starting at `2026-05-08T00:13:43Z`, suburban's `cloudflared` process logs
hundreds of failures dialing `[3d06:bad:b01:200::1]:8006` (the PVE web UI
exposed via Cloudflare Tunnel). This is consistent with the address being
removed seconds earlier.

## Hypotheses ruled out

1. WAN flap on Comcast or Webpass: vmbr0's v6 RA-assigned address is intact
   and the default route is fresh. Suburban can ping `2606:4700:4700::1111`
   via `mwan` watchdog probes (logs every 30 seconds, no failures).
2. Wireguard tunnel flap (`wg0`): `mwan-ifmgr` shows `wg0` peer
   `jz3eKGui` with `handshake_age=2s` at 00:13:53, healthy throughout.
3. NDP cache or kernel bridge state: `dmesg` only shows the standard tap
   port forwarding-state churn from VM 950 cycling, plus the
   `hash_elasticity` deprecation notice. Nothing else.
4. New `vmbrtrunk` bridge stealing forwarding: `bridge -d link show` confirms
   `vmbrtrunk` has no ports attached and is unrelated to vmbr1's traffic.

## Mitigation suggestion (not executed)

The minimal surgical fix is to re-add the v6 address on suburban directly:

```
ssh -o HostName=10.240.0.148 suburban \
  'ip -6 addr add 3d06:bad:b01:200::1/64 dev vmbr1'
```

That restores transit instantly. To make the fix persist across the next
network reload, also add the line back into `/etc/network/interfaces` under
the `iface vmbr1 inet6 static` stanza:

```
iface vmbr1 inet6 static
    address 3d06:bad:b01:200::1/64
    address fe80::1/64
```

The longer-term fix is to either:

- Import vmbr1 into Tofu state with the full v6 address list, so future
  applies preserve it; or
- Manage vmbr1 outside Tofu (since Tofu's Proxmox provider rewrites the
  whole stanza on every apply, anything not in state gets dropped).

## Conditional untaint outcome

V6 SSH to suburban (`ssh suburban`, which resolves to AAAA
`3d06:bad:b01:200::1`) still times out:

```
$ ssh -o ConnectTimeout=8 suburban 'echo ok-v6'
ssh: connect to host 3d06:bad:b01:200::1 port 22: Operation timed out
```

Per the investigation rules, `tofu untaint proxmox_network_linux_bridge.trunk`
is NOT executed while v6 transit remains broken. The untaint is deferred
until v6 transit is restored.
