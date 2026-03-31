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

Ansible runs from either the CLI on the `ansible` container (`3d06:bad:b01::107`, which
has `PROXMOX_API_TOKEN` set) or via the Semaphore UI at `https://ansible.home.goodkind.io`.
The vault password lives at `~/.config/ansible/vault.pass` on the controller and as
`ANSIBLE_VAULT_PASSWORD` in the Semaphore environment.

Playbooks live in `ansible/playbooks/` and follow a `deploy-<service>.yml` naming
convention. Run them from the `ansible/` directory with `ansible-playbook`. Use
`--limit <hostname>` to target a single host and `--check --diff` for a dry run.

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

