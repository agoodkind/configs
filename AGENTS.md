## AGENTS

This is the infrastructure configuration repository for `goodkind.io`. It contains Ansible
playbooks for LXC/VM provisioning, network device configs (Traefik, KEA DHCP, BIND), the
multi-WAN load balancer setup, and operational docs for the homelab.

The primary deployment target is a single Proxmox VE host named `vault` at
`3d06:bad:b01::254`, running all LXC containers and QEMU VMs. A secondary Proxmox host
named `suburban` at `3d06:bad:b01:200::254` runs test and auxiliary workloads in NJ.

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

`service_mapping.yml` in `ansible/inventory/group_vars/all/` is the single source of
truth for container hostnames and IPv6 addresses. The dynamic inventory plugin and all
templates derive from it.

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

---

## SSH Access Quick Reference

All commands use `root` unless otherwise noted. IPv6 literals require brackets in URLs but not
in bare `ssh` commands.

| Host              | Exact SSH command                                | Method                  | Notes                                                                                                             |
| ----------------- | ------------------------------------------------ | ----------------------- | ----------------------------------------------------------------------------------------------------------------- |
| OPNsense router   | `ssh agoodkind@3d06:bad:b01::1`                  | Direct IPv6             | User is `agoodkind`, not root. Use `sudo` for privileged tasks.                                                   |
| vault (Proxmox)   | `ssh root@3d06:bad:b01::254`                     | Direct IPv6             | Proxmox host itself.                                                                                              |
| proxy (110)       | `ssh -p 2222 root@3d06:bad:b01::110`             | Direct IPv6, port 2222  | SSHPiper runs on port 22 of this container; sshd is on 2222. Alternatively `ssh root@proxy@ssh.home.goodkind.io`. |
| mwan (VM 113)     | `ssh root@mwan@ssh.home.goodkind.io`             | SSHPiper                | Also reachable directly: `ssh root@3d06:bad:b01::113`.                                                            |
| debianct (100)    | `ssh root@debianct@ssh.home.goodkind.io`         | SSHPiper                |                                                                                                                   |
| unifi (102)       | `ssh root@unifi@ssh.home.goodkind.io`            | SSHPiper                |                                                                                                                   |
| dns64 (103)       | `ssh root@dns64@ssh.home.goodkind.io`            | SSHPiper                |                                                                                                                   |
| grommunio (104)   | `ssh root@grommunio@ssh.home.goodkind.io`        | SSHPiper                |                                                                                                                   |
| pvd (105)         | `ssh root@pvd@ssh.home.goodkind.io`              | SSHPiper                |                                                                                                                   |
| consul (106)      | `ssh root@consul@ssh.home.goodkind.io`           | SSHPiper                |                                                                                                                   |
| ansible (107)     | `ssh root@ansible@ssh.home.goodkind.io`          | SSHPiper                | Also the Ansible controller; has `PROXMOX_API_TOKEN` set.                                                         |
| freebsd-dev (108) | `ssh root@freebsd-dev-home@ssh.home.goodkind.io` | SSHPiper                | Short name is `freebsd-dev-home`, not `freebsd-dev`.                                                              |
| mc (109)          | `ssh root@mc@ssh.home.goodkind.io`               | SSHPiper                |                                                                                                                   |
| adguard (112)     | `ssh root@adguard@ssh.home.goodkind.io`          | SSHPiper                |                                                                                                                   |
| home-assistant    | `ssh root@10.250.2.3 -p 22222`                   | Direct IPv4, port 22222 | HAOS SSH add-on on port 22222. Standard sshd is not present.                                                      |
| mini              | `ssh agoodkind@3d06:bad:b01:1::2`                | Direct IPv6             | User is `agoodkind`. Not managed via SSHPiper.                                                                    |
| nas               | `ssh nas <command>`                              | SSH config alias        | Ubuntu 24.04.3, user `agoodkind`. Resolves via `~/.ssh/config`. Not managed via SSHPiper.                         |
| suburban          | `ssh suburban`                                   | SSH config alias        | Proxmox VE NJ hypervisor. Resolves to `3d06:bad:b01:200::254`. User `root`.                                       |
| berylax           | `ssh berylax`                                    | SSH config alias        | OpenWrt GL.iNet router. `berylax.goodkind.io` resolves to `3d06:bad:b01:300::1`.                                  |

For any LXC container not in the table: the pattern is `ssh root@<shortname>@ssh.home.goodkind.io`
where `<shortname>` is the hostname prefix from `service_mapping.yml`. The `pct exec` fallback
via vault works for all LXCs regardless of SSHPiper status.

SSHPiper listens on port 22 on the proxy container. Two items worth noting: the SSHPiper config
routes `suburban` to a stale address; the live management address is `3d06:bad:b01:200::254`.
Direct access via the SSH alias works because `ssh_config.local` maps `suburban` correctly.

---
