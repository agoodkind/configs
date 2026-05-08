# Plan: Enable BGP Graceful Restart on mwan-agent and OPNsense FRR

## Context

When we deployed the new mwan binary to VM 113 today, restarting `mwan-agent` caused a measured BGP outage of ~1.7 seconds on OPNsense (session drop -> reconverge to the LXC 116 backup -> session re-established when the new agent reconnected). Long-running TCP connections that NAT through VM 113's WAN broke during that window because LXC 116 egresses through a different external IP.

This is not how production-grade BGP stacks handle planned control-plane restarts. The standard mechanism is **BGP Graceful Restart (RFC 4724)**: the restarter advertises a capability that says "if my session drops, hold my routes for N seconds before withdrawing", and the helper (OPNsense FRR in our case) does exactly that. The data plane (Linux kernel routing on VM 113) keeps forwarding packets the entire time, so a graceful restart is invisible to traffic. Cilium and Calico both rely on this pattern for their per-pod-restart upgrade story.

GoBGP v4.4.0 (which we use) has full GR support in its proto API; OPNsense's `os-frr` plugin already templates `bgp graceful-restart` gated on a single config flag. We just have not wired it up on either side.

**Outcome:** future planned restarts of `mwan-agent` (binary upgrades, config reloads, deploys) will not cause a traffic blip. OPNsense holds the routes for the GR window. Failure modes (real WAN failure, agent panic) are unchanged because the existing watchdog still detects connectivity loss and explicitly withdraws via gRPC.

**Scope choices made (defaults):**
- **GR only.** RFC 4724. Ipv4-unicast and ipv6-unicast.
- **LLGR (RFC 9494) is out of scope.** File as a follow-up if restarts ever take minutes. Not needed for our 1-3s deploys.
- **BFD (RFC 5880) is out of scope** for this ticket but worth a follow-up ticket (file via tack, let it assign the ID) since it would close the "GR considered harmful" gap on real link failures.
- **Restart-time = 30s.** Long enough for our deploys; short enough to limit black-hole risk if WAN dies during the window.
- **OPNsense side:** REST API call (curl-driven shell script tracked in repo). The `os-frr` plugin's Jinja2 template at `/usr/local/opnsense/service/templates/OPNsense/Quagga/bgpd.conf` already emits `bgp graceful-restart` when `OPNsense.quagga.bgp.graceful == '1'`. We toggle that flag via the API.

## Approach

### Side A: mwan Go binary

Add GR settings to the `[bgp]` section of all four config locations, plumb them through the `BGPSection` struct into the speaker's `Config`, and set the corresponding GoBGP v4 fields when building the global config, the per-peer config, and the `StopBgpRequest`.

The shutdown path also changes: when GR is enabled, `agent/main.go` must NOT pre-emptively call `WithdrawDefault()` before `Stop()`, because that defeats GR. Instead `Stop()` calls `StopBgp` with `AllowGracefulRestart: true`, which makes GoBGP skip the BGP CEASE notification and trigger GR semantics on the helper.

### Side B: OPNsense FRR

A short shell script under `mwan/scripts/opnsense-bgp-graceful-toggle.sh` (or similar) that:

1. Reads OPNsense API credentials from `~/.config/opnsense/api.env` (or wherever the existing tooling stores them; reuse if there is an existing script).
2. POSTs to `/api/quagga/bgp/set` with the `graceful` flag set to `1`.
3. POSTs to `/api/quagga/service/reconfigure` to apply.
4. Verifies with `vtysh -c "show running-config router bgp"` over SSH that the `bgp graceful-restart` line appears.

The script becomes the source-of-truth invocation; AGENTS.md gets a one-paragraph "how to enable" pointer.

## Files to change

### New
- `mwan/scripts/opnsense-bgp-graceful-toggle.sh` (new shell script for the OPNsense REST API call).
- `mwan/go/internal/bgp/speaker_test.go` (new; covers GR config propagation into the GoBGP API calls and the AllowGracefulRestart shutdown path).

### Modified
- `mwan/go/internal/config/config.go`
  - Add `GracefulRestart` sub-struct to `BGPSection`. Fields: `Enabled bool`, `RestartTime uint32`, `NotificationEnabled bool`. (`DeferralTime` we can leave at GoBGP default; document it but do not expose unless we need it.)
  - Add validation in `validateBGP`: if `Enabled` is true, require `RestartTime > 0` and `<= 600`.

- `mwan/go/internal/bgp/config.go`
  - Mirror the new fields on `Config`.

- `mwan/go/internal/bgp/speaker.go`
  - In `Start()` (line 75): when `s.cfg.GracefulRestart.Enabled` is true, set `apipb.StartBgpRequest.Global.GracefulRestart` with `Enabled`, `RestartTime`, `NotificationEnabled`. Reference: GoBGP `apipb.GracefulRestart` struct.
  - In `addPeer()` (line 132): when GR is enabled, set `peer.GracefulRestart` (so it is advertised per-neighbor) and set `MpGracefulRestart.Config.Enabled = true` on each `apipb.AfiSafi` entry (the existing v4 and v6 branches at lines 147-167).
  - In `Stop()` (line 182): when GR is enabled, set `apipb.StopBgpRequest{ AllowGracefulRestart: true }` (current call passes empty struct).

- `mwan/go/internal/agent/main.go`
  - Pass new GR fields from `cfg.BGP` into `bgp.Config` (around line 75).
  - In the SIGTERM path (line 184): skip the `WithdrawDefault()` call when `cfg.BGP.GracefulRestart.Enabled` is true. The current behavior of pre-withdrawing is correct only when GR is OFF.

- `mwan/config/production.toml.j2`
- `mwan/config/suburban-testbed.toml.j2`
- `mwan/production/lxc-116/config.toml` (literal file, not a template)
- `mwan/testbed/lxc-100/config.toml` (literal file)

Each gets a new `[bgp.graceful_restart]` block:

```toml
[bgp.graceful_restart]
enabled = true
restart_time = 30
notification_enabled = true
```

For the two `.j2` templates, prefer rendering from Ansible variables so prod and testbed can diverge if needed:

- `ansible/inventory/group_vars/mwan_servers.yml`: add `bgp_graceful_restart_enabled: true`, `bgp_graceful_restart_restart_time: 30`, `bgp_graceful_restart_notification_enabled: true`.
- `ansible/inventory/group_vars/mwan_testbed_servers.yml` (or wherever testbed vars live): same defaults.

### Documentation
- `AGENTS.md`: add a short subsection under "MWAN deployment topology" called "BGP graceful restart" that says the feature is enabled, restart_time is 30s, and points at `mwan/scripts/opnsense-bgp-graceful-toggle.sh` for the OPNsense side.

## Verification

### Unit / build
- `cd mwan/go && make check && make test`. The new `speaker_test.go` should cover:
  - GR fields in `Config` propagate into `apipb.StartBgpRequest.Global.GracefulRestart` (capture via a fake `BgpServer` interface or assert via a side-channel; speaker.go does not currently abstract `server` so this may need a small interface refactor).
  - `Stop()` passes `AllowGracefulRestart: true` when GR is enabled.

### Testbed end-to-end (suburban + VM 950 + LXC 100)
1. Deploy new binary + new config to suburban host, VM 950, LXC 100 using the existing AGENTS.md "Manual rollout" recipe.
2. Run the OPNsense GR toggle script against the testbed OPNsense instance (if there is one; otherwise apply against prod since this is read-only at the FRR-state level until a peer restarts).
3. From OPNsense: `sudo vtysh -c "show bgp neighbors 3d06:bad:b01:201::3"`. The output should include `Graceful Restart Capability: advertised and received` and `Restart Time: 30 seconds`.
4. SSH to suburban; restart `mwan-agent` on VM 950: `ssh -J suburban root@3d06:bad:b01:200::950 'systemctl restart mwan-agent'`.
5. Concurrently on OPNsense: `sudo vtysh -c "show ip bgp ::/0"`. The route should NOT disappear during the restart window. The neighbor should briefly show as restarting, with the path marked stale, then return to normal.
6. From a host on the LAN side, run a sustained ping6 / curl loop during the restart and confirm zero loss.

### Production (after testbed verification)
1. Run the OPNsense GR toggle script against prod OPNsense.
2. Verify `show bgp neighbors` advertises the GR capability for both `3d06:bad:b01:fe::3` (VM 113) and `3d06:bad:b01:fe::4` (LXC 116).
3. Restart `mwan-agent` on LXC 116 (zero-impact backup). Confirm the new code path does not accidentally withdraw routes pre-emptively. Watch OPNsense logs for graceful semantics.
4. Restart `mwan-agent` on VM 113 during a quiet window. Watch a sustained ping6 and an SSH session for loss. Expectation: zero packet loss; SSH stays up.
5. Roll back via `cp -a /usr/local/bin/mwan.prev /usr/local/bin/mwan && systemctl restart mwan-agent` if anything is wrong.

## Risk and mitigations

- **GR considered harmful without BFD.** If a real WAN link failure happens during the 30s GR window, OPNsense holds VM 113's route and traffic black-holes for that period. Mitigations: (a) restart-time is 30s not 120s; (b) the existing watchdog detects connectivity loss and calls `WithdrawRoutes` via gRPC, which forces an immediate BGP UPDATE WITHDRAW even with GR enabled; (c) the BFD follow-up ticket (to be filed via tack) closes this gap properly.
- **Speaker test refactor.** `speaker.go` currently holds a concrete `*server.BgpServer` (no interface). Writing a speaker test cleanly may require introducing a small interface. Keep the refactor minimal: just enough to swap the server for a fake in tests.
- **OPNsense GR config plugin compatibility.** The `os-frr` plugin's bgpd.conf.j2 template emits the `bgp graceful-restart` line gated on the `OPNsense.quagga.bgp.graceful` flag. We have not confirmed the exact REST API endpoint name (`/api/quagga/bgp/set` is a guess based on the plugin's convention). Verification step #2 above flushes this out; if the endpoint is named differently the script gets one-line fixed.

## Execution: parallel subagents in isolated worktrees

This work is decomposed into independent slices, each owned by its own subagent in its own git worktree, the same pattern used for MWAN-121..125. The integrator (main session) creates the worktrees off `origin/main`, dispatches the subagents in parallel, then merges their branches into a golden branch in the order below. Each subagent operates only inside its worktree path and commits to its own branch. No subagent reaches outside its slice.

The TOML field names and the Go struct names are part of THIS plan, so every subagent works from a single shared spec. They do not need to coordinate with each other at runtime.

**Locked spec (every subagent uses these exact names):**
- TOML block: `[bgp.graceful_restart]` with keys `enabled`, `restart_time`, `notification_enabled`.
- Go struct: `BGPSection.GracefulRestart` of type `BGPGracefulRestart` with fields `Enabled bool` (toml `enabled`), `RestartTime uint32` (toml `restart_time`), `NotificationEnabled bool` (toml `notification_enabled`). Mirrored on `bgp.Config` as `GracefulRestart bgp.GracefulRestartConfig`.
- Default values: `enabled = true`, `restart_time = 30`, `notification_enabled = true`.

### Slice 1: `mwan-126-subtask-a` (Go code: config schema + speaker wiring + tests)

Owns:
- `mwan/go/internal/config/config.go` (add `BGPGracefulRestart` struct + field on `BGPSection`; validation in `validateBGP`).
- `mwan/go/internal/bgp/config.go` (mirror struct).
- `mwan/go/internal/bgp/speaker.go` (Start, addPeer, Stop changes per the locked spec).
- `mwan/go/internal/bgp/speaker_test.go` (new; may require a small interface to fake the GoBGP server).

Tightly coupled because the speaker wiring is what the config shape exists for.

### Slice 2: `mwan-126-subtask-b` (Agent shutdown conditional)

Owns:
- `mwan/go/internal/agent/main.go` lines 73-100 (pass GR config from `cfg.BGP.GracefulRestart` into the speaker constructor) and lines 183-185 (skip `WithdrawDefault()` when `cfg.BGP.GracefulRestart.Enabled` is true).

Independent of Slice 1's internals; only depends on the locked TOML field name and the locked Go struct field name.

### Slice 3: `mwan-126-subtask-c` (TOML config rendering)

Owns:
- `mwan/config/production.toml.j2` (add `[bgp.graceful_restart]` block).
- `mwan/config/suburban-testbed.toml.j2` (same).
- `mwan/production/lxc-116/config.toml` (literal file, add the same block).
- `mwan/testbed/lxc-100/config.toml` (literal file, add the same block).
- `ansible/inventory/group_vars/mwan_servers.yml` (add three `bgp_graceful_restart_*` variables).
- `ansible/inventory/group_vars/mwan_testbed_servers.yml` if it exists (same variables for testbed).

No Go code. No speaker code. Only config rendering.

### Slice 4: `mwan-126-subtask-d` (OPNsense REST API toggle script)

Owns:
- `mwan/scripts/opnsense-bgp-graceful-toggle.sh` (new shell script that POSTs to `/api/quagga/bgp/set` with the `graceful` flag and reconfigures via `/api/quagga/service/reconfigure`).
- A short README block at the top of the script documenting the API key prerequisite and the verify step.

Fully independent of all Go work. Can run in parallel from the start.

### Slice 5: `mwan-126-subtask-e` (AGENTS.md documentation update)

Owns:
- `AGENTS.md`: a short subsection under "MWAN deployment topology" called "BGP graceful restart" describing what is enabled, the restart-time value, and pointing at the slice-4 script for the OPNsense side.

Independent of all other slices. Can run in parallel from the start.

### Integration

After all five slices commit on their branches, the integrator merges in order: Slice 1 -> Slice 2 -> Slice 3 -> Slice 4 -> Slice 5 into a `mwan-126-golden` branch off `origin/main`. Expected merge conflicts: minimal, since slices 1, 2, 3 touch disjoint files; the only possibility is a small `internal/agent/main.go` overlap if Slice 1's speaker test refactor changes the speaker constructor signature in a way Slice 2 also needs to call.

The integrator runs `make check && make test && make build-linux && make build-mwan-opnsense` on the golden branch, then surfaces the result for user review before any merge into local main and before any deploy.

### Hard rules for every subagent

- Subagent operates only inside its worktree path. No edits outside its assigned files.
- No `ansible-playbook`, no `systemctl restart`, no `scp` to any host. Local repo only.
- No commits to `main` or any branch other than the slice's own branch.
- No em-dashes in code, comments, or commit messages.
- `make check` and `make test` (where applicable) must pass on the slice's branch before the agent reports done.

## Critical files for the implementer

- `mwan/go/internal/bgp/speaker.go:75-84` (StartBgp call)
- `mwan/go/internal/bgp/speaker.go:132-170` (addPeer)
- `mwan/go/internal/bgp/speaker.go:182` (StopBgp)
- `mwan/go/internal/bgp/config.go:4-28` (Config struct)
- `mwan/go/internal/config/config.go` (BGPSection; line numbers around 174-196 per earlier exploration)
- `mwan/go/internal/agent/main.go:73-100, 183-185` (speaker construction + shutdown)
- `mwan/config/production.toml.j2` (BGP block around lines 121-143)
- `mwan/config/suburban-testbed.toml.j2` (BGP block around lines 117-138)
- `mwan/production/lxc-116/config.toml` (BGP block at the top of the file)
- `mwan/testbed/lxc-100/config.toml`

## Out of scope (file as separate tickets)

- **BFD investigation (file via tack, no pre-picked number).** Status as of 2026-05-07: we ship `gobgp/v4@v4.4.0` which has no BFD surface. Upstream `v4.5.0` (released 2026-04-30) added BFD primitives: an RFC 5880 packet codec, OpenConfig-aligned schema, and an API alignment. It is unclear without reading the v4.5.0 source whether the full BFD daemon (session establishment + integration with BGP peer state) ships or whether v4.5.0 is just the foundational codec/schema. The investigation should: (1) bump our dependency to v4.5.0 and inspect the new API surface; (2) decide whether v4.5.0 is sufficient or whether we still need to run FRR's `bfdd` standalone alongside; (3) confirm the OPNsense side (which already has `neighbor X bfd` in its running config) can speak BFD with whatever we deploy; (4) prototype on testbed before any prod change.
- **LLGR follow-up (file via tack).** Add Long-Lived GR with `LLGR_STALE` and `NO_LLGR` community handling. Only worth doing if/when restart durations grow past the GR window.
- **OPNsense FRR config in repo.** Track the full FRR config in this repo and push via Ansible. Big lift; fights the `os-frr` plugin's auto-generation. Defer.
