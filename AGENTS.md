## AGENTS

This is the infrastructure configuration repository for `goodkind.io`. It contains Ansible
playbooks for LXC/VM provisioning, network device configs (Traefik, KEA DHCP, BIND), the
multi-WAN load balancer setup, and operational docs for the homelab.

The primary deployment target is a single Proxmox VE host named `vault` in San Francisco at  
`3d06:bad:b01::254`, running all LXC containers and QEMU VMs. A secondary Proxmox host
named `suburban` runs test and auxiliary workloads in NJ.

## Sources of Truth

- **Infrastructure state** (IPs, bridges, services, tunnels, open issues): `INFRA.md`
- **Container/VM hostnames and IPv6 addresses**: `ansible/inventory/group_vars/all/service_mapping.yml`
- **Static inventory and host groups**: `ansible/inventory/hosts`
- **Dynamic Proxmox inventory**: `ansible/inventory/proxmox.yml`
- **Per-service variables**: `ansible/inventory/group_vars/<service>_servers.yml`
- **Shared variables**: `ansible/inventory/group_vars/all/vars.yml`
- **Secrets** (encrypted): `ansible/inventory/group_vars/all/vault.yml`
- **SSH access, network topology, Cloudflare config**: `INFRA.md`

## Deployment Workflow

**New containers are provisioned by OpenTofu** (see `opentofu/`). Run `tofu apply` from
that directory first, then run the corresponding Ansible playbook to configure the
container. Existing containers (pre-OpenTofu) are still created by Ansible's
`create-ct.yml` until they are migrated. The Plane container (VMID 115) is the current
pilot; its `deploy-plane.yml` no longer imports `setup-service-ct.yml` because OpenTofu
owns provisioning.

OpenTofu state is stored in Consul at `opentofu/state`. Credentials go in
`opentofu/terraform.tfvars` (gitignored; see `terraform.tfvars.example`).

Ansible runs **locally** from `ansible/` on the controller (this machine). Vault password
lives at `~/.config/ansible/vault.pass`.

Playbooks live in `ansible/playbooks/` and follow a `deploy-<service>.yml` naming
convention. See `.cursor/commands/deploy-playbook.md` for the exact invocation. Use
`--limit <hostname>` to target a single host and `--check --diff` for a dry run.

## Surgical Change Protocol

Production hosts (vault, mwan, OPNsense, berylax) serve live traffic for non-technical
users who cannot recover from outages. Physical access to hardware is unavailable for months
at a time. Treat every change as potentially irreversible.

**Before any change to a production host:**

1. **Understand the current state.** SSH in and read live config, routes, rules, logs.
   Do not trust INFRA.md or Ansible templates as ground truth; they drift.
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

**Things that have gone wrong before:**
- Watchdog emailing on every probe cycle because gRPC port was firewalled (port 50052
  missing from nftables input chain).
- PD-sourced traffic misrouting via wrong WAN because source-based ip6 rules were missing
  from update-routes.sh.
- IA_NA addresses having partial reachability (some destinations unreachable) which is
  normal and does not affect PD-based forwarding.

## Monolith Architecture

All Go infrastructure code lives in a single binary: `mwan/go/cmd/mwan/`. This binary
has three subcommands:

- `mwan agent` — gRPC agent running inside the MWAN VM (VM 113)
- `mwan watchdog` — connectivity monitor and rollback daemon on vault
- `mwan cutover` — HA cutover tool (preflight, migrate, start-backup, verify, rollback)

There are NO separate Go binaries. Everything is one binary with `internal/` packages
for separation of concerns. Each subsystem (watchdog, cutover, agent) is its own package
under `internal/`. Shared code lives in `internal/config`, `internal/email`,
`internal/logging`, `internal/ops`. The entry point at `cmd/mwan/main.go` dispatches to
subcommand packages.

This is a hard requirement. Do not create separate binaries for new functionality.
New tools become subcommands of the monolith. This ensures:
- One build artifact to deploy
- Shared logging, config, and library code
- No version drift between components
- `mwan-unfuck` works from any path (it calls `mwan cutover rollback`)

The `mwan cutover start-backup` phase fully configures the failover LXC (forwarding,
masquerade, routes, keepalived) from scratch. The LXC is treated as disposable — the
monolith owns all its configuration.

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
- **Secrets in Ansible Vault.** TOML templates use `{{ mwan_smtp2go_api_key }}` Jinja2
  variables. Never commit plaintext secrets. The `.j2` suffix signals a template.
- **Linting enforced.** `make lint` (golangci-lint) must pass. Config in `mwan/go/.golangci.yml`.
- **Cutover is one-time.** `mwan cutover` is a one-time migration tool. After production
  cutover, it will be deprecated. Ongoing failover is handled by `mwan watchdog failover`.

## Rules for Changes

1. Before editing any playbook or template, check the Ansible quality rules in
  `.cursor/rules/ansible-quality.mdc`. It documents common pitfalls around single-bracket
   tests, `set_fact` concurrency, folded block scalars in URLs, and guard clause patterns.
2. Shell scripts in `mwan/scripts/` must use `[[ ]]` for tests, full `if/then/fi` blocks
  with no inline ternaries, and pass `shellcheck --severity=error`. The full style
   requirements are in `.cursor/rules/mwan.mdc`.
3. Secrets go in `ansible/inventory/group_vars/all/vault.yml` (Ansible Vault encrypted).
  Never commit plaintext secrets anywhere in the repo. For new services provisioned via
   OpenTofu, per-service generated secrets (db passwords, secret keys) may use Ansible's
   `lookup('password', ...)` plugin, which caches values in `<service>/.secrets/`
   (gitignored) on the Ansible controller.
4. IPv6 is P0. The diagnosis workflow is in `.cursor/rules/ipv6-dhcp-diagnosis.mdc`.
5. The `kea/` Rakefile is the live mechanism for pushing DHCP config to the router.
  Do not modify KEA config files without understanding the Rake deploy step first.

## Emergency OOB access

When vault's network is down (MWAN VM stopped, routing broken), SSH to vault is unavailable.
The fallback is a USB-serial cable from berylax (`/dev/ttyUSB0`) to vault's physical serial
port. Full procedure and prerequisites are in `INFRA.md` under "Emergency out-of-band (OOB)
access".

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

---

