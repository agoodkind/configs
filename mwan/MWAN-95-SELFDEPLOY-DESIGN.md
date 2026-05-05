# MWAN-95 Self-Bootstrapping Deploy: design

## Goal

After a one-time SSH bootstrap of the OPNsense daemon, all subsequent
upgrades flow over the gRPC channel itself. No SSH dependency for
upgrades. Self-healing on bad deploy: auto-revert to previous binary
if new one is unreachable.

## Non-goals

- Authenticated deploy. The bridge socket on the Proxmox host is
  root-only via filesystem perms. That IS the trust boundary.
- Bridge daemon self-deploy. Bridge runs on Linux with full tooling
  (Ansible, systemd, package manager). It ships through the canonical
  Ansible path like every other Linux daemon in the repo.
- Cross-version protocol negotiation. Both sides upgrade in lockstep
  via Ansible+gRPC. Mixed-version operation is out of scope.

## Trust model

| Layer | Boundary |
|---|---|
| Bridge unix socket on Proxmox host | mode 0660 root:root. Only root processes on the host can call any RPC, including Deploy. |
| qemu virtio-serial chardev | only the bridge daemon ever connects to it (single-listener qemu chardev semantics). |
| OPNsense daemon | trusts the bridge unconditionally. Bridge is the only thing that ever talks to it. |

A compromised root on the Proxmox host owns OPNsense already. Vector
exists via qm exec, config.xml access via /var/lib/vz, and snapshot
rollback. Adding auth at the gRPC layer doesn't change that reality.
Filesystem-perm gate is sufficient.

## On-disk layout (OPNsense daemon)

```
/usr/local/sbin/mwan-opnsense           -> symlink to current binary
/usr/local/sbin/mwan-opnsense.current   -> the active binary (rename target)
/usr/local/sbin/mwan-opnsense.previous  -> the prior binary (kept for revert)
/usr/local/sbin/mwan-opnsense.staged    -> incoming binary, written atomically
/var/run/mwan-opnsense.deploy.state     -> colon-delim flag file (see below)
```

Symlink discipline:

- `mwan-opnsense` is what rc.d invokes. It always points at `.current`.
- Atomic swap is `rename(.staged, .current)` after fsync, then update symlink.
- Revert is symlink `mwan-opnsense -> .previous`, signal daemon to re-exec.

## State file format

Plain colon-delimited `key:value`, one per line. Idiomatic for sh/awk parsing,
no JSON parser dependency. Confirmed `jq` is not in OPNsense base and
installing it conflicts with the "no pkg dependencies" constraint.

```
# /var/run/mwan-opnsense.deploy.state
active:8b2f7d9a3c1...sha256
previous:1a4e6b8c2d0...sha256
version:0.4.2-dirty+abc123
deployed_at:1714879200
health:ok
```

Parse with `awk -F: '$1=="health"{print $2}' < state`. Or for the
common boolean check: `awk -F: '$1=="health" && $2=="ok"' state | grep -q .`.

State file is written atomically: write to `.state.tmp`, fsync, rename.
Reads tolerate transient absence (preflight assumes no-state means fresh boot).

## Deploy RPC

```proto
service OpnsenseService {
  rpc Deploy(DeployRequest) returns (DeployReply);
  rpc DeployStatus(DeployStatusRequest) returns (DeployStatusReply);
  rpc Revert(RevertRequest) returns (RevertReply);
}

message DeployRequest {
  bytes  binary       = 1;  // full ELF, no compression (it's local IPC)
  string sha256_hex   = 2;  // verified before swap
  string version_str  = 3;  // free-form, e.g. git describe output, for state file
}

message DeployReply {
  string staged_sha256 = 1;
  string previous_path = 2;  // for caller's audit
  bool   re_exec_started = 3;
}
```

## Deploy state machine (daemon side)

1. `Deploy(binary, sha256, version)`:
   - Write payload to `mwan-opnsense.staged.tmp`, fsync.
   - Verify sha256.
   - chmod 0755.
   - `rename(.staged.tmp, .staged)` for atomicity.
   - `cp .current .previous` (overwrite previous; keep one revert step).
   - `rename(.staged, .current)` for atomicity.
   - Update `/var/run/mwan-opnsense.deploy.state` with old/new/timestamps.
   - Reply with re_exec_started=true.
   - Spawn watchdog goroutine that sleeps 500ms then `syscall.Exec(argv[0])`.
     This replaces the process image. rc.d sees no exit because exec is not fork.

2. After re-exec:
   - New daemon starts, opens /dev/ttyV0.1 same as before.
   - Within 30s, must answer one self-RPC. The same serial fd cannot
     work for self-call. See "Self-check mechanism" below.
   - On success: stamp `.deploy.state` with `health=ok`. Retire `.previous`
     after a 1-hour soak. Don't delete during soak in case operator
     wants manual revert. Soak gives plenty of time to catch latent
     post-startup failures (e.g. issues that only surface under traffic).
   - On failure (no self-RPC within 30s, or panic during boot, or rc.d
     respawn count exceeds 3 in 60s): rc.d's daemon -r will respawn.
     Each respawn loads `.current` which is the bad binary. So:

3. Pre-exec failsafe: before `syscall.Exec(.current)`, the OUTGOING (still-good)
   daemon writes a marker `/var/run/mwan-opnsense.pending-verify`. New daemon
   on startup checks this marker. If present, it:
   - Spawns a self-check timer (30s).
   - On self-check fail OR panic, the supervisor wrapper script (see below)
     sees the marker and the failed exit. It reverts the symlink to .previous
     before next respawn.

## Supervisor (rc.d) responsibility

rc.d uses `daemon -r` to respawn on exit. We add a wrapper script that
runs before each daemon start:

```sh
# /usr/local/etc/rc.d/mwan-opnsense.preflight
#!/bin/sh
PENDING=/var/run/mwan-opnsense.pending-verify
HEALTH=/var/run/mwan-opnsense.deploy.state
SBIN=/usr/local/sbin

if [ -f "$PENDING" ]; then
    # Previous start was a fresh deploy that may have failed.
    # Check if it became healthy.
    if awk -F: '$1=="health" && $2=="ok"' "$HEALTH" 2>/dev/null | grep -q .; then
        rm -f "$PENDING"
    else
        # Bad deploy. Revert.
        logger -t mwan-opnsense-deploy "self-check failed; reverting"
        cp "$SBIN/mwan-opnsense.previous" "$SBIN/mwan-opnsense.current"
        rm -f "$PENDING"
    fi
fi
```

This runs as part of the rc.d start hook. Respawn-via-daemon-r calls back
to the rc.d script which calls preflight first.

## Self-check mechanism

The daemon can't dial its own serial port. Single-connection qemu
chardev means the bridge owns it. Self-check options:

| Option | Note |
|---|---|
| Daemon writes "I'm up" to `.deploy.state` after grpc.Server.Serve returns from initial setup | Simple. Doesn't actually verify the gRPC server is functional. |
| Daemon registers a hook in its own gRPC server that fires on the first incoming RPC of any kind | Verifies real RPC plumbing. But "first incoming RPC" depends on the bridge sending one, which is coupling. |
| Bridge sends a `Version` ping immediately after seeing the daemon's reconnect, marks the deploy healthy | The right answer. End-to-end verification, no in-process self-call needed. |

Going with option 3: the **bridge** is the health checker, not the daemon
itself. Bridge already needs reconnect logic (gRPC keepalive). On
successful reconnect during a pending-verify window, it calls Version
and writes `health=ok` to the daemon's state file via... wait, no, it
can't write to the daemon's filesystem.

Revised: bridge calls a `DeployStatus` RPC after Version succeeds. The
daemon's DeployStatus handler updates its own state file. The pending
marker is cleared by the daemon itself in this handler.

```
bridge: detects new deploy (got DeployReply.re_exec_started=true)
bridge: starts heartbeat probe loop with exponential backoff
        delays: 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, 5s, 5s, 5s, ...
        cap: 5s, total budget: 60s
bridge: each iteration tries Version()
bridge: first success -> calls DeployStatus(MarkHealthy)
daemon: DeployStatus handler clears /var/run/mwan-opnsense.pending-verify
        and writes health=ok to .deploy.state
bridge: if 60s budget elapses without any Version success, calls Revert RPC
        (or, if Revert is unreachable too, the rc.d preflight kicks in
        on the next respawn cycle anyway. Defense in depth.)
```

Exponential backoff catches the happy path (sub-second daemon startup)
on the first or second attempt, while still tolerating up to 60s of
genuine startup slowness. Fixed-cadence at 2s would miss the fast case
by averaging an extra second of latency per deploy.

## Revert RPC

```proto
message RevertRequest {}
message RevertReply { string reverted_to_sha256 = 1; }
```

Daemon side: copy `.previous` over `.current`, write marker, exec.
Same re-exec dance as Deploy. After successful self-check, retire
the (now-current, formerly-previous) binary's `.previous` slot.

## Bridge update path (Ansible)

Bridge runs on Proxmox host. Linux, full tooling. Standard pattern:

```
ansible/roles/mwan-opnsense-host/
  templates/
    mwan-opnsense-host.service.j2   # systemd unit, Restart=always
  tasks/main.yml                     # copies binary, restarts unit
```

Operator runs `ansible-playbook -t mwan-opnsense-host` to upgrade the
bridge. systemd handles restart, gRPC keepalive on the daemon side
detects the disconnect and waits for fresh handshake.

The daemon survives a bridge restart cleanly. Its grpc.Server tears
down the dead conn, stays Listening on the persistent fd, accepts
the new bridge's handshake when it reconnects.

## Initial bootstrap (first install)

Cold-start, no daemon yet:

1. Build mwan-opnsense binary on macOS (cross-compile).
2. scp via existing ad-hoc SSH path (one time only).
3. install -m 0755 to `/usr/local/sbin/mwan-opnsense.current`.
4. ln -sf .current `/usr/local/sbin/mwan-opnsense`.
5. Drop rc.d script + rc.conf.d enable.
6. service mwan-opnsense start.

After this, **all upgrades flow via gRPC Deploy**. SSH access can be
disabled on OPNsense (or fail across version upgrades) and we stay
operational.

## Failure modes covered

| Failure | Recovery |
|---|---|
| Deploy bytes corrupted in transit | sha256 verify before swap, never reaches .current |
| New binary panics on startup | rc.d respawn + pending-verify marker + preflight reverts symlink |
| New binary starts but gRPC server broken | bridge can't get Version, calls Revert (or preflight kicks in next respawn) |
| Bridge crashes mid-deploy | daemon's pending-verify marker stays. Preflight reverts on next respawn even without bridge intervention |
| Both daemons die | Proxmox-host operator can ssh to OPNsense and manually `cp .previous .current`. Last resort, requires SSH, acceptable. |
| Disk full during stage | rename atomic, .staged.tmp leftover cleaned on next deploy attempt, .current never touched |

## What this does NOT cover

- Daemon configuration changes. Only binary swap. Config changes via
  ConfigUpdate RPC are separate and not in scope here.
- OPNsense base OS upgrades. Orthogonal. OPNsense package manager
  handles its own. We only own /usr/local/sbin/mwan-opnsense.
- Multi-step migrations across major daemon versions. Deploy is a
  drop-in binary swap. Backward-compat schema is the daemon's
  responsibility, not the deploy mechanism's.

## Effort estimate

| Phase | Effort |
|---|---|
| Add Deploy/DeployStatus/Revert to proto + regen | 30 min |
| Daemon-side handlers (deploy.go, supervisor.go) | 2-3 hr |
| Bridge-side heartbeat probe + Revert call | 1 hr |
| rc.d preflight script | 30 min |
| Testbed end-to-end test (deploy good binary, deploy bad binary, observe revert) | 1 hr |
| Total | About 5-6 hours |

## Decisions locked in

- State file: colon-delimited flag file, awk-parseable, no jq dependency.
- Soak: 1 hour before retiring `.previous`.
- Heartbeat: exponential backoff, 100ms initial, 5s cap, 60s total budget.
