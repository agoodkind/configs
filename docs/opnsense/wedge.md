# The mid-transfer write wedge

A bridge disconnect while the guest is writing to the host can hang the guest until you hard
reset the VM. The mechanism below is the best-evidenced explanation, with the evidence named
beside each claim. Reproduction depends on a guest write being in flight, so treat it as a real
risk on guest-to-host transfers, not a guaranteed repro. For the daemon and transport this
wedge constrains, see [the OOB daemon doc](daemon.md).

## Trigger

The trigger needs a guest-to-host write in flight when the host side drops. A `file pull` or a
large command output keeps the guest writing host-ward, so a bridge restart or crash during one
can wedge the guest. A `file push` rarely wedges, because the guest is mostly reading. In
testing, more than a dozen mid-push bridge kills produced no wedge, while mid-pull bridge
restarts wedged on most genuine trials (three of four in one run).

## Symptoms

The gRPC channel stops answering and both guest vCPUs peg in the kernel. sshd, the QEMU Guest
Agent, and the serial console all stop responding, because no thread gets a CPU slice.

## Mechanism

The evidenced mechanism is a single guest write stranded in the kernel.

- A guest write to the virtio-serial port enters the FreeBSD driver, takes the port mutex
  `vtcpmtx`, and busy-spins in `virtqueue_poll` waiting for the host to consume the buffer.
  Evidence: live `info registers` sampling on a wedged VM held CPU0 in `virtqueue_poll` across
  repeated reads.
- When the host chardev disconnects mid-write, qemu does not complete that in-flight
  descriptor, so the poll never returns. Evidence: `info chardev` showed the port
  `disconnected` during the wedge while the write never finished. This matches a known qemu
  limitation present through qemu 11.0 (Red Hat bug 1352977).
- A second guest thread needs `vtcpmtx` and busy-spins for it. Evidence: the second vCPU sat in
  `lock_delay`, and a guest memory dump showed the `vtcpmtx` lock held by the stuck write
  thread.

Both vCPUs then spin in the kernel and the guest starves. The write is an uninterruptible
kernel spin, so you cannot signal or kill it.

## Recovery

Only a hard reset clears it, because that recreates the qemu device. Run `qm reset <vmid>`. A
bridge restart alone and a guest daemon restart do not clear it.

Do not restart the bridge while a deploy or any guest-to-host transfer is in flight.

## Inspect a wedged guest

Userland is starved, so sshd, the guest agent, and the serial-console shell do not respond. Use
the kernel debugger instead. Set `debug.kdb.break_to_debugger=1` on the guest, then run
`echo nmi | qm monitor <vmid>` from the Proxmox host. The guest enters DDB on the serial console
and dumps every thread's backtrace, which you read over the `serial0` socket.

DDB on the stock kernel shows each thread's stack but cannot name the owner of a contended
lock, because the stock OPNsense kernel ships without `WITNESS`. To attribute `vtcpmtx` to the
thread that holds it, build a debug kernel as below.

## Build a kernel-debug (WITNESS) build

A WITNESS kernel lets DDB print lock ownership with `show alllocks` and `show lockchain`, which
turns "a thread is spinning in `lock_delay`" into "this thread holds `vtcpmtx`". The stock
kernel already has `DDB`, `KDB`, and `DDB_CTF`; you only add `WITNESS` and `DEBUG_LOCKS`.

Match the source to the running kernel. OPNsense builds its kernel from `github.com/opnsense/src`
at the tag for the release. For OPNsense 26.1.2 (FreeBSD 14.3-RELEASE-p8) the tag is `26.1.1`;
confirm with `sysctl kern.conftxt` and the kernel git hash it prints.

1. Free disk. The build needs roughly 5 to 10 GB. Grow the VM disk from the Proxmox host if
   needed: `qm disk resize <vmid> scsi0 +24G`, then on the guest `gpart recover da0`,
   `gpart resize -i 4 da0`, and `zpool online -e zroot da0p4`.
2. Stage the source. The guest has no internet, so clone `opnsense/src` at the tag on the host
   and copy it into the guest `/usr/src`, or clone directly if the guest can reach GitHub.
3. Write the config. In `/usr/src/sys/amd64/conf`, create a file `SMP-WITNESS`:

   ```
   include SMP
   ident SMP-WITNESS
   options WITNESS
   options DEBUG_LOCKS
   makeoptions DEBUG=-g
   ```

4. Build the kernel: `make -j2 buildkernel KERNCONF=SMP-WITNESS`. On a 2-vCPU VM this takes
   about an hour.
5. Install it beside the stock kernel, never over it:
   `make installkernel KERNCONF=SMP-WITNESS KODIR=/boot/kernel.witness`. The stock
   `/boot/kernel` stays as the fallback.
6. Boot it deliberately. `nextboot -k kernel.witness` did not take on this OPNsense loader in
   testing; it booted the stock kernel instead. Set the kernel in `/boot/loader.conf.local`
   with `kernel="kernel.witness"`, or pick it at the loader prompt over the `serial0` console.
7. Verify the debug kernel is live: `uname -i` prints `SMP-WITNESS`, and
   `sysctl debug.witness.watch` exists. That OID is absent on the stock kernel.
8. Capture lock ownership. Induce a wedge, enter DDB with `echo nmi | qm monitor <vmid>`, then
   run `show alllocks` and `show lockchain` to read which thread owns `vtcpmtx`.
9. Revert. Remove the `kernel="kernel.witness"` line from `/boot/loader.conf.local` and reboot
   to return to the stock kernel. The stock `/boot/kernel` was never modified.
