# AGENTS

This is the infrastructure configuration repository for `goodkind.io`. It contains Ansible
playbooks for LXC/VM provisioning, network device configs (Traefik, KEA DHCP, BIND), the
multi-WAN load balancer setup, and operational docs for the homelab.

Current IPs, bridges, services, tunnels, SSH access, network topology, and
Cloudflare config live in [docs/infra/INFRA.md](docs/infra/INFRA.md).

## Sources of Truth

- **Infrastructure state** (IPs, bridges, services, tunnels): [docs/infra/INFRA.md](docs/infra/INFRA.md)
- **Container/VM hostnames and IPv6 addresses**: `ansible/inventory/group_vars/all/service_mapping.yml`
- **Static inventory and host groups** (hypervisors, OPNsense SSH targets):
  `ansible/inventory/hosts`
- **Dynamic Proxmox inventory** (one file per hypervisor):
  `ansible/inventory/vault.proxmox.yml`, `ansible/inventory/suburban.proxmox.yml`
- **Per-target variables**: `ansible/inventory/group_vars/<group>_servers.yml`
- **Shared non-secret variables**: `ansible/inventory/group_vars/all/vars.yml`
- **Secrets** (encrypted): `ansible/inventory/group_vars/all/vault.yml`

## Deployment Workflow

**New containers are provisioned by OpenTofu** (see `opentofu/`). Run `tofu apply` from
that directory first, then run the corresponding Ansible playbook to configure the
container. Existing containers (pre-OpenTofu) are still created by Ansible's
`create-ct.yml` until they are migrated.

OpenTofu state is stored in Consul at `opentofu/state`. Credentials go in
`opentofu/terraform.tfvars` (gitignored; see `terraform.tfvars.example`).

Five canonical playbooks own the MWAN/Proxmox/OPNsense surface; each one targets
a specific group so prod-vs-testbed differences come from group_vars, not from
playbook conditionals:

- `playbooks/deploy-proxmox.yml`: Proxmox hypervisor work (mwan-ifmgr,
  mwan-watchdog, cloudflared-oob). Target `proxmox_servers`.
- `playbooks/deploy-mwan.yml`: MWAN VM (prod VM 113 on vault). Target
  `mwan_servers`.
- `playbooks/deploy-mwan-failover.yml`: MWAN failover LXC. Target
  `mwan_failover_servers` (prod) or `mwan_failover_test_servers` (testbed).
- `playbooks/deploy-opnsense.yml`: mwan-opnsense daemon. Target
  `opnsense_servers` (prod) or `opnsense_test_servers` (testbed).
- `playbooks/deploy-testbed.yml`: suburban-only extras (`qm args`, ISP LXCs,
  host bridge, VFIO). Target `suburban_servers`.

Other `deploy-<service>.yml` playbooks (proxy, adguard, ddns, tack, etc.) follow
the same convention but target a single service. Always use `--limit <host>` for
production runs and `--check --diff` for dry runs first. The local `ansible/Rakefile`
wraps the canonical five with shortcuts (`rake deploy:proxmox[vault]`,
`rake check:mwan`, `rake syntax:all`).

The deploy-playbook workflow lives in [.agents/skills/deploy-playbook/SKILL.md](.agents/skills/deploy-playbook/SKILL.md).
Ansible setup and inventory contracts live in [docs/ansible/ANSIBLE.md](docs/ansible/ANSIBLE.md). Vault rules and the safe
key-listing command live in [docs/ansible/SECRETS.md](docs/ansible/SECRETS.md).

## Surgical Change Protocol

Production hosts (vault, mwan, OPNsense) serve live traffic for non-technical
users who cannot recover from outages. Berylax is indefinitely offline for now;
historical host and OOB notes live in [docs/BERYLAX.md](docs/BERYLAX.md). Physical
access to hardware is unavailable for months at a time. Treat every change as
potentially irreversible.

**Before any change to a production host:**

1. **Understand the current state.** SSH in and read live config, routes, rules, logs.
   Do not trust [docs/infra/INFRA.md](docs/infra/INFRA.md) or Ansible templates as ground truth; they drift.
2. **Form a testable hypothesis.** State what you expect the change to do and what would
   prove it worked.
3. **Test surgically.** Apply the smallest possible change, verify with a specific command,
   then remove it. Example: add one ip6 rule, verify route lookup changed, run one ping,
   remove the rule.
4. **Verify no regression.** After confirming the fix, check that forwarded traffic, load
   balancing, and other paths still work before making anything permanent.
5. **Then codify.** Only after the live test passes, write the change into the Ansible
   template or script in the repo.
6. **Never bulk-change production.** No `ansible-playbook` runs against mwan without
   verifying each component independently first. No `systemctl restart` of networking
   services without a rollback plan.

## Operational Pointers

- Current vault hypervisor state lives in [docs/infra/vault.md](docs/infra/vault.md).
- Current MWAN host topology, live unit names, repository layout state, manual rollout order,
  stale binary inventory, and cleanup notes live in [docs/infra/mwan-layout.md](docs/infra/mwan-layout.md).
- Current suburban testbed state lives in [docs/infra/suburban-testbed.md](docs/infra/suburban-testbed.md).
- Current non-vault host state lives in [docs/infra/hosts.md](docs/infra/hosts.md).
- Current OPNsense network topology lives in [docs/infra/opnsense.md](docs/infra/opnsense.md).
- Current Cloudflare account, tunnel, WARP route, load balancer, and DNS state lives in
  [docs/infra/cloudflare.md](docs/infra/cloudflare.md).
- MWAN design, BGP failover, graceful restart, watchdog behavior, and snapshot rules live in
  [docs/MWAN.md](docs/MWAN.md).
- OPNsense BGP steady state, operational foot-guns, and recovery snippets live in
  [docs/runbooks/OPNSENSE-OPERATIONAL-NOTES.md](docs/runbooks/OPNSENSE-OPERATIONAL-NOTES.md).
- OPNsense config-import internals live in
  [docs/runbooks/opnsense-25.7-config-import-flow.md](docs/runbooks/opnsense-25.7-config-import-flow.md).
- The OPNsense testbed config-import gate lives in
  [docs/runbooks/opnsense-testbed-config-import.md](docs/runbooks/opnsense-testbed-config-import.md).
- MWAN email and alert routing lives in [docs/plans/mwan-email-routing.plan.md](docs/plans/mwan-email-routing.plan.md).
- Berylax is indefinitely offline for now. Historical berylax host, Cloudflare,
  and OOB serial notes live in [docs/BERYLAX.md](docs/BERYLAX.md).
- Emergency out-of-band access state lives in [docs/infra/oob.md](docs/infra/oob.md).

## Monolith Contract

All Go infrastructure code lives in one binary built from `mwan/go/cmd/mwan/`. The
linux/amd64 build is `mwan`; the freebsd/amd64 build is `mwan-opnsense` and runs only
on OPNsense, where it auto-dispatches into the `opnsense` daemon based on its argv[0].

There are NO separate Go binaries. New tools become subcommands of this monolith.
Shared code lives under `internal/config`, `internal/email`, `internal/logging`,
`internal/ops`, `internal/bgp`, `internal/alert`, `internal/tracing`, `internal/mwn1`,
`internal/rollback`.

Current MWAN command surfaces and host layout live in [docs/infra/mwan-layout.md](docs/infra/mwan-layout.md).
MWAN failover and watchdog behavior live in [docs/MWAN.md](docs/MWAN.md).

## Build rules for implementation agents

Every implementation agent, whether dispatched as a subagent or running inline, must apply these:

- **Start from evidence.** Read the relevant source before changing code. Read this file and any local design doc the change touches. Do not assume architecture from names alone.
- **Respect the boundary.** Generic layers stay generic. Provider-specific or platform-specific behavior lives behind the provider boundary. Preserve exact user-visible values unless an external boundary requires escaping or translation.
- **Implement the real behavior.** Wire features into the real runtime path, not only into tests or fallback code. Prefer one source of truth over compatibility crutches. Reconcile related state immediately when the user-facing contract says values should stay in sync. Avoid deferred cleanup.
- **Avoid shortcuts.** No baseline edits to hide lint findings. No `//nolint` without explicit operator authorization. No synthetic references, dummy logs, or marker-method calls to satisfy reachability tools. No no-op closers or empty lifecycle methods. No compile-only or log-only tests presented as behavioral coverage.
- **Keep types tight.** Avoid `any`, `interface{}`, and loose maps unless required at a real external boundary. Convert untyped input to concrete types as early as possible.
- **Write useful tests.** Test the real contract. Add regression coverage for the failure mode that motivated the change. Avoid tests that only prove compilation, only log output, or assert implementation trivia.
- **Preserve project hygiene.** Keep edits inside scope. Do not revert unrelated work. Update comments and docs when they would otherwise describe the old contract.
- **Verify before reporting.** Run the project's real gates: `make check`, `make test`, `make build-linux`, `make build-mwan-opnsense`. State exactly what was run and whether it passed. If a gate could not be run, state why.
- **Report honestly.** State what changed. State the verification commands. State residual risks. Do not claim files, symbols, commits, or behavior that was not verified. Every factual claim must trace to a command run in this session with the output cited verbatim. No "likely", "probably", or "should" without a verifying command.

## LLM Writing Guidelines

Treat every statement here as guidance for how to write and ingest material in this repo, not
as asserted fact about any specific host. If anything conflicts with a primary source (a file
in git, a man page, or output you reproduced), prefer the primary source and treat this
document as stale until someone updates it.

**Default stance:** Assume claims are uncertain until tied to evidence. Prefer "it appears" when
the basis is a single file or log snippet. Prefer "this suggests" when inferring from several
weak signals. State "no verifiable source is available" explicitly rather than filling gaps
with confidence.

**Evidence discipline:** For each non-trivial statement, tie it to a repo path, a command with
representative output, or an external URL. When treating something as proof, name the evidence
first, then give the conclusion qualified by that evidence.

**Investigatory tone:** Write as if the reader is joining an ongoing investigation. Prefer "it
may be worth checking" over "you must". Offer options and describe what to observe if someone
tries them.

**Conflicts between sources:** List disagreements without forcing resolution. Note which source
is usually authoritative for that layer only if a cited policy or comment in the repo supports
it. Suggest a single reproducible check that would break the tie if one exists.

**Staleness:** Every statement should carry implicit scope (environment, date or git ref if
known, and what would make the note obsolete). Infra drifts; "last verified" beats
"timeless truth".

**Secrets:** Never copy tokens, passwords, private keys, or session cookies. Refer to vault keys,
env var names, or rotation procedures. If a secret's location must be described, use path plus
permission model, not content.

**Shell and code notes:** For shell, note bash vs zsh when expansion or builtins differ. For
Ansible, follow the `Ansible Quality Checks` section below. For mwan scripts,
follow the `MWAN / OPNsense Working Rules` section below.

## Prose rule

Prose reads cleanly as a linear record of the thing itself. Each sentence is a full sentence with a concrete subject, a concrete verb, and enough context to sound natural when spoken aloud. Each new sentence adds useful information in the same direction as the sentence before it, with low cognitive load and no hidden context the reader must reconstruct. Paragraphs move forward by accumulation, with no setup, interruption, reversal, or correction.

## Go Code Standards

These rules apply to all Go code in `mwan/go/`. Violations block merge.

- **Single TOML config.** All subcommands read `/etc/mwan/config.toml`. No env-var-based
  config loading. Env vars override secrets only (`SMTP2GO_API_KEY`, `PVE_TOKEN_SECRET`).
- **No globals.** Config is passed explicitly through function arguments. No package-level
  `var` for config, state, or singletons.
- **DRY.** No duplicated structs, no bridge/adapter types that mirror another struct
  field-by-field. If two things need the same data, they share one type.
- **Small files.** No file over 500 lines. If a file exceeds this, split by responsibility.
- **Separated concerns.** Config loading, business logic, I/O, and CLI parsing live in
  separate files. No function that parses flags AND runs business logic.
- **One email sender.** One `EmailSender` type, parameterized at construction. No
  per-subcommand email implementations.
- **One logger factory.** One `newLogger()` function parameterized by subcommand name, log
  paths, and optional email handler. No per-subcommand logger setup files.
- **No hardcoded values.** IPs, paths, timeouts, email addresses, hostnames come from TOML
  config. Validation errors loudly if a required field is missing.
- **Comments explain WHY, not WHAT.** Do not add comments that restate the code. Do not add
  `// Foo does X` when the function name already says X.
- **Secrets in Ansible Vault.** Templates that need secrets reference `vault_*`
  values directly. Never commit plaintext secrets. The `.j2` suffix signals a
  template.
- **Linting enforced.** `make lint` (golangci-lint) must pass. Config in `mwan/go/.golangci.yml`.
- **Cutover is complete.** The `mwan cutover` and `mwan cutover2` subcommands have
  been removed from the binary. Ongoing failover is handled by `mwan watchdog failover`.

## Rules for Changes

1. Before editing any playbook or template, check the `Ansible Quality Checks` section below. It documents common pitfalls around single-bracket
   tests, `set_fact` concurrency, folded block scalars in URLs, and guard clause patterns.
2. Shell scripts in `mwan/scripts/` must use `[[ ]]` for tests, full `if/then/fi` blocks
   with no inline ternaries, and pass `shellcheck --severity=error`. The full style
   requirements are in the `MWAN / OPNsense Working Rules` section below.
3. Secret values go in `ansible/inventory/group_vars/all/vault.yml` (Ansible Vault
   encrypted) under `vault_*` names. Shared non-secret defaults go in
   `ansible/inventory/group_vars/all/vars.yml`. Do not add pure aliases like
   `foo: "{{ vault_foo }}"` to shared or service `group_vars` files. If a
   consumer needs env fallback, keep that wrapper local to the service or play. For
   new services provisioned via OpenTofu, per-service generated secrets (db
   passwords, secret keys) may use Ansible's `lookup('password', ...)` plugin,
   which caches values in `<service>/.secrets/` (gitignored) on the Ansible
   controller.
4. IPv6 is P0. The diagnosis workflow is in the `IPv6 and DHCP Diagnosis Guidelines` section below.
5. The `kea/` Rakefile is the live mechanism for pushing DHCP config to the router.
   Do not modify KEA config files without understanding the Rake deploy step first.
6. Always invoke git with `-C /path/to/repo` because shell cwd is unreliable across
   worktrees and subshells. A bare `git push` or `git commit` can land in the wrong
   repo. The agent-gate hook blocks raw `git` invocations.
7. `ansible-vault view` output is sensitive. Do not run it in a way that can reach the
   conversation log, and do not pipe it through `grep`, `awk`, or similar filters. If you
   only need variable names, use `python3 scripts/ansible_vault_keys.py --vault-password-file ~/.config/ansible/vault.pass ansible/inventory/group_vars/all/vault.yml`.
8. Capture forensics when the runbook says STOP. Do not improvise, do not retry, and
   surface the state to the operator.

## IPv6 and DHCP Diagnosis Guidelines

### IPv6 Priority

- IPv6 connectivity issues are P0 and should be treated as critical.
- Prefer IPv6 literals over IPv4 in configuration files and bindings.
  Use `[::1]` instead of `127.0.0.1`, and use `[::]` instead of
  `0.0.0.0`.
- When diagnosing or verifying connectivity with SSH, curl, or similar tools,
  test IPv6 addresses first.
- Check the IPv6 stack first, then check IPv4.

### DHCPv6 Reservation Matching in KEA

Containers and VMs can receive different IPv6 addresses despite MAC-based
reservations when the DHCPv6 DUID changes, stale leases remain active, or KEA
cannot match the identifier used by the reservation.

DUIDs can change for these common reasons:

1. `DUIDType=vendor` is based on `/etc/machine-id`, which can change when a
   container is recreated.
2. `DUIDType=link-layer` is based on the MAC address and stays stable when the
   MAC address is pinned.
3. `DUIDType=uuid` is based on product UUID and may change.

KEA matching depends on these settings:

- `mac-sources` controls how KEA extracts MAC addresses from DHCPv6 packets.
  `duid` only works with DUID-LL values such as
  `00:03:00:01:XX:XX:XX:XX:XX:XX`. `ipv6-link-local` extracts the MAC from an
  EUI-64 link-local address. `client-link-addr-option` uses DHCPv6 option 79
  when a relay provides it.
- `host-reservation-identifiers` controls the identifier order KEA checks.
  The list must include the identifier type used by the reservation.
- DHCPv6 reservations should use either `duid` or `hw-address`, not both.
  Prefer `hw-address` when the MAC is pinned and KEA can extract it.

Use this decision tree for DHCPv6 reservations:

1. If the DUID is stable and uses DUID-LL, use a DUID reservation and set
   `host-reservation-identifiers: ["duid", "hw-address"]`.
2. If the DUID changes, use an `hw-address` reservation and confirm
   `mac-sources` can extract the MAC address.
3. If the client sends DUID-EN (`00:02:...`), `mac-sources: ["duid"]` will not
   extract the MAC. Use `ipv6-link-local` or relay support instead.

Stable systemd-networkd DHCPv6 DUID generation requires this block:

```ini
[DHCPv6]
DUIDType=link-layer
```

DUID-LL remains stable only while the MAC address remains stable.

### DHCPv6 Diagnosis Workflow

Check the current container or VM state first:

```bash
ssh root@<host> 'ip -6 addr show eth0'
ssh root@<host> 'networkctl status eth0 | grep -i duid'
ssh root@<host> 'ip link show eth0 | grep -i link/ether'
```

Check the KEA lease database next:

```bash
ssh agoodkind@3d06:bad:b01::1 'sudo grep -i <mac> /var/db/kea/kea-leases6.csv'
```

Look for multiple DUIDs for the same MAC. DUID-EN starts with `00:02`, and
DUID-LL starts with `00:03:00:01`.

Verify KEA reservation settings:

```bash
grep -A 2 "host-reservation-identifiers" /path/to/kea-dhcp6.conf
grep "mac-sources" /path/to/kea-dhcp6.conf
```

Clean stale leases by MAC when DUID instability created multiple leases:

```bash
rake lease:cleanup6_by_mac[bc:24:11:1d:2c:0f,10]
```

Clean by DUID or IP only when that narrower identifier is known:

```bash
rake lease:cleanup6_conflicts[00:03:00:01:bc:24:11:1d:2c:0f,10]
rake lease:delete6_by_ip[3d06:bad:b01::66]
```

Renew DHCP after cleanup:

```bash
ssh root@<host> 'networkctl renew eth0'
```

Restart `systemd-networkd` only when renewal is insufficient and the restart is
safe for the host being changed.

Verify that the reservation is honored:

```bash
ssh root@<host> "ip -6 addr show eth0 | grep 'scope global'"
ssh agoodkind@3d06:bad:b01::1 'sudo grep <expected-ip> /var/db/kea/kea-leases6.csv'
```

### DHCPv6 Common Fixes

When a container gets a different IP each time, check current DUID, lease
history, DUID format, MAC pinning, and stale lease entries before changing
configuration.

When a reservation exists but is not honored, check whether the reservation uses
`hw-address` while the client sends DUID-EN, whether the reservation uses `duid`
while the DUID changed, whether stale leases still exist, and whether
`host-reservation-identifiers` includes the identifier type used in the
reservation.

When DUID stability cannot be fixed, hardcode DUID only as a last resort:

```ini
[DHCPv6]
DUIDType=link-layer
DUIDRawData=bc:24:11:1d:2c:0f
```

Then use the full DUID in the KEA reservation:

```json
{
  "duid": "00:03:00:01:bc:24:11:1d:2c:0f",
  "ip-addresses": ["3d06:bad:b01::c"]
}
```

Before committing IPv6 or DHCP changes, verify `host-reservation-identifiers`,
verify `mac-sources`, verify `DUIDType=link-layer`, verify MAC pinning, check
for stale leases, and test reservation matching after deployment.

## SSH / Host Access Rules

### Proxy Container Access

The proxy container (`proxy.home.goodkind.io`) has two SSH entry points:

1. Port 22 is SSHPiper and routes public SSH traffic to other containers based
   on username, for example `ssh user@ssh.home.goodkind.io`.
2. Port 2222 is OpenSSH and gives direct administrative access to the proxy
   container itself, for example `ssh -p 2222 root@3d06:bad:b01::110`.

To access the proxy container itself through SSHPiper, use the `@proxy` suffix
when it is configured:

```bash
ssh root@proxy@ssh.home.goodkind.io
ssh root@proxy
```

That shortcut matches `^(.+)@proxy` in `sshpiperd.yaml` and routes to
`127.0.0.1:2222`.

### Primary SSH Method

All `*.ssh.home.goodkind.io` DNS points to `3d06:bad:b01::110`, where SSHPiper
routes by username:

```bash
ssh adguard@ssh.home.goodkind.io
ssh pdns@ssh.home.goodkind.io
ssh mwan@ssh.home.goodkind.io
```

Use the pattern `ssh <short-hostname>@ssh.home.goodkind.io`. The short hostname
is extracted from the full container name, such as `adguard` from
`adguard.home.goodkind.io`.

### Direct IP and Jump Host Access

When SSHPiper is unavailable or troubleshooting requires a direct path, use IPv6
directly:

```bash
ssh root@<ipv6-address>
```

If the IP address is unknown, look it up from Proxmox:

```bash
ssh root@3d06:bad:b01::254
pct list | grep -i <name>
qm list | grep -i <name>
pct exec <VMID> -- ip addr show eth0
qm guest cmd <VMID> network-get-interfaces
ssh root@<ipv6-address>
```

Use OPNsense as a jump host only as a last resort:

```bash
ssh agoodkind@3d06:bad:b01::1
ssh root@3d06:bad:b01::254
ssh root@<container-ip>
```

Known access points:

- Proxy VM: `3d06:bad:b01::110`, SSHD on port 2222 and SSHPiper on port 22.
- OPNsense: `3d06:bad:b01::1`, SSH user `agoodkind`, use `sudo` for privileged
  tasks.
- Proxmox host: `3d06:bad:b01::254`, SSH user `root`.
- Ansible container: `3d06:bad:b01::107`, CT 107, with `PROXMOX_API_TOKEN` set.

Disable strict host key checking only for automation or diagnostics:

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@<ip>
```

## MWAN / OPNsense Working Rules

### SSH and Host Access

Use jump hosts explicitly when the environment requires them. A typical chain is
`ssh agoodkind@3d06:bad:b01::1`, then `ssh root@3d06:bad:b01::254`, then the
VM or host. Disable strict host key checking only when instructed and only for
automation or diagnostics.

Prefer non-disruptive actions on the MWAN VM. Avoid `systemctl restart
systemd-networkd` unless it is necessary.

### Deterministic Convergence

Scripts and services must be idempotent and tolerate partial boot ordering. Any
script that mutates shared kernel state must serialize writers with a lock. Use
`flock` under `/run/...` for `ip rule`, `ip route`, and `nft` writers.

When a service can run before prerequisites are ready, gate it with an
`ExecStartPre` wait script and configure retries with `Restart=on-failure`.

### Jinja and Templating

Do not keep a `.j2` file if it contains no Jinja syntax (`{{`, `{%`, or `{#}`).
Use a normal file extension and deploy it with `ansible.builtin.copy`.

If a shell script, hook, or service only uses templating to inject variables,
prefer a single templated env file such as `/etc/mwan/mwan.env`, make the script
or hook static, and source the env file. For systemd units, prefer
`EnvironmentFile=/etc/mwan/mwan.env`.

### WAN State Terminology

Use `healthy`, `unhealthy`, and `unknown` for WAN health. Do not use `up` or
`down` for health because those terms are confusable with `ip link` state.

### Logging and JSON

Prefer structured JSON logs to `/var/log/mwan-debug.log`. Use `jq -cn` for JSON
generation, avoid `printf`-built JSON, avoid embedded Python for JSON or
parsing, and include `traceId` when `MWAN_TRACE_ID` is available.

### Parsing and Tooling

Prefer machine-readable outputs:

- `ip -j ... | jq` over text parsing from `ip`.
- `networkctl ... --json=short | jq` over text parsing.
- `ipcalc-ng --all-info -j ... | jq` over bespoke IPv6 math.
- `nft -j` for inspection where feasible.

Avoid long pipe trains such as `... | grep ... | tail -1 | awk ... | awk ...`.
Prefer a single-pass `awk` program or capture output to local variables. Keep
scripts portable across Debian base utilities unless GNU-only behavior is
already installed and assumed.

Use `jq` for selection, extraction, and light reshaping. Avoid algorithmic
formatting in `jq`, such as byte-array to IPv6 conversion. Move that logic to a
small shell helper.

If `ipcalc-ng ... | jq -r '.NETWORK'` repeats, factor it into a helper such as
`ipcalc_field <cidr> <jq_field>` or `ipcalc6_field <cidr> <jq_field>`.

### Shell Style and Safety

Use `set -euo pipefail` for scripts that mutate state. Use `|| true` only at
expected failure points such as best-effort probes or cleanup. Source constant
paths directly, for example `. /etc/mwan/mwan.env`, to keep shellcheck happy.
Avoid unused `ENV_FILE` variables.

Keep lines reasonably short. Wrap long `{ ... }` blocks with newlines after
`{`. For every MWAN script or hook, include a top comment block that explains
what it does and where it sits in the dependency graph.

### Console Auto-login

Physical console access through keyboard, mouse, VGA, or serial auto-logs in as
root without a password. SSH access still requires normal authentication.

The getty overrides are:

- `getty@tty1`: `--autologin root --noclear %I $TERM`.
- `serial-getty@ttyS0`: `--autologin root --keep-baud 115200,57600,38400,9600 - ${TERM}`.

Ansible handles directory creation for idempotent deployment, and systemd
reloads on reboot.

### nftables and Runtime Rules

Assume an `nftables` reload flushes runtime rules. Dynamic runtime rule
programming such as NPT must be re-applied through `networkd-dispatcher` hooks
and a boot or deploy safety-net systemd unit.

### Documentation Constraints

Do not add new docs unless asked. When updating existing docs, avoid giant
pasted code blocks. Prefer short excerpts and direct pointers to files. When an
MWAN issue is encountered and fixed, document it in
[docs/MWAN.md#troubleshooting](docs/MWAN.md#troubleshooting) using the
established format.

## Ansible Quality Checks

### Debugging: Fix Root Causes, Not Symptoms

When a variable is missing or validation fails, investigate why before adding
defensive code.

Adding `| default('')` or `when: var is defined` without understanding why the
variable is missing masks the real issue. Use `when` for logic branches, not for
defensive programming.

Before adding `| default()` or `is defined`, answer these questions:

1. Where is this variable supposed to come from?
2. Is there a naming mismatch, such as `proxmox_type` versus `proxmox_vmtype`?
3. Is the variable missing from inventory composition?
4. Is the variable set in a different play that has not run yet?
5. Has the source been fixed before a defensive fallback is added?

Example anti-patterns and fixes:

```yaml
# BAD - bandaid that hides the real problem
type: "{{ proxmox_vmtype | default('lxc') }}"

# BAD - defensive when that masks missing data
- name: Configure service
  ansible.builtin.template:
    src: config.j2
    dest: /etc/service/config
  when: service_config is defined

# GOOD - when for an actual logic branch
- name: Configure IPv6
  ansible.builtin.template:
    src: ipv6.j2
    dest: /etc/network/ipv6
  when: enable_ipv6 | bool

# GOOD - fix at the source
# compose:
#   proxmox_vmtype: proxmox_type
```

### Line Length Limits

Prefer YAML lines below 80 columns. Lines up to 90 columns are acceptable. The
hard limit is 120 columns. Break long lines with YAML block scalars, Jinja2
string concatenation, variable extraction, or conditionals split at logical
points.

When wrapping long templated values, use Jinja2 whitespace control such as `-}}`
and `{{-` so YAML folding does not insert unwanted spaces.

### YAML Formatting Issues

For folded block scalars with URLs, remember that `>-` replaces newlines with
spaces. Use a single-line quoted string for simple URLs. For long URLs that must
be broken, use Jinja2 whitespace control to avoid adding spaces.

Do not put surrounding quotes inside a `url: >-` or `Authorization: >-` folded
scalar. The quotes become literal characters and can produce errors such as
`unknown url type: "https`.

Use `block: |` for `blockinfile` content that contains `ExecStart` or other
commands. Do not include a literal `>-` inside the block content.

Do not use YAML block scalar operators as shell line continuation inside
`shell: |` blocks. In shell text, `>-` is a literal argument. Prefer
`ansible.builtin.command` with `argv:` or real shell line continuations.

Example command pattern:

```yaml
- ansible.builtin.command:
    argv: ["htpasswd", "-nbB", "{{ user }}", "{{ pass }}"]
  register: htpasswd_out
  changed_when: false
  no_log: true

- ansible.builtin.copy:
    dest: /etc/traefik/auth/dashboard-users
    content: "{{ htpasswd_out.stdout }}\n"
    mode: "0600"
  no_log: true
```

Systemd-networkd does not merge duplicate sections. Only the first section is
processed. Do not use `blockinfile` when it might create duplicate sections such
as `[DHCPv6]`. Use `lineinfile` with `regexp` and `insertafter`, or ensure the
section does not exist before using `blockinfile`.

### Variable Safety

Do not reference variables in a `set_fact` task that are defined in the same
task. Ansible sets those values concurrently. Split dependent facts into
sequential tasks.

```yaml
# BAD - var1 is not available to var2 in the same task
- set_fact:
    var1: "value"
    var2: "{{ var1 }}-suffix"

# GOOD
- set_fact:
    var1: "value"

- set_fact:
    var2: "{{ var1 }}-suffix"
```

Do not use the `first` filter on a list without checking length. Use array
indexing with a length check:

```yaml
ip: "{{ ip_list[0] if (ip_list | length > 0) else '' }}"
```

### KEA DHCP Configuration

In DHCPv4 reservations, `hw-address` and `client-id` are mutually exclusive. In
DHCPv6 reservations, `hw-address` and `duid` are mutually exclusive. Use only
one identifier per reservation. Prefer `hw-address` when the MAC address is
pinned.

### Secrets Management

Follow a vault-first policy. Do not rely on local controller files such as
`/var/lib/semaphore/...` existing. Store secrets in
`inventory/group_vars/all/vault.yml`, and inject them with
`content: "{{ vault_var }}"` rather than `src:`.

### Dynamic List Parsing

Ansible `uri` can return multi-line strings for list-like data such as
Cloudflare IP ranges. Split strings explicitly into lists with `splitlines()`:

```yaml
trusted_ips: "{{ response.content.splitlines() }}"
```

### Process Management

When debugging or running complex deployments that might race, use `--forks=1`
to force sequential execution. Ensure previous `ansible-playbook` processes are
killed before starting a new run if they might be hung.

### Linting and Verification

`ansible-lint` is not instant. Wait 2 to 5 seconds after making changes before
checking linter output. If linter errors appear immediately after a fix, wait a
few seconds and re-check.

Before committing Ansible changes, verify these points:

1. YAML line length stays below the hard limit of 120 columns.
2. No `url:` fields use `>-` with multi-line URLs unless whitespace control is
   correct.
3. No `url: >-` or `Authorization: >-` values contain surrounding quotes.
4. No `shell: |` blocks contain YAML operators like `>-` as line continuation.
5. No `blockinfile` blocks contain literal `>-` in command output.
6. No `set_fact` task references variables defined in the same task.
7. No `first` filter is used without empty-list checks.
8. No KEA reservations use mutually exclusive identifiers.
9. No `blockinfile` task creates duplicate systemd-networkd sections.
10. No `| default()` is added without first investigating why the variable is
    missing.
