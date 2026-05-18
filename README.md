# configs

Infrastructure configuration management for `goodkind.io`.

For how to operate in this repo (deployment workflow, SSH access, rules for changes), see [AGENTS.md](AGENTS.md). For a point-in-time snapshot of the running environment (hosts, WAN links, Cloudflare), see [docs/infra/INFRA.md](docs/infra/INFRA.md).

## Quick start

Dependencies are managed with Bundler. Install them before running any Rake tasks.

Ansible playbooks run from the `ansible/` subdirectory. The intended controller is the ansible container at `3d06:bad:b01::107`, which has `PROXMOX_API_TOKEN` set and the vault password in place. Playbooks can also be triggered via the Semaphore UI at `ansible.home.goodkind.io`.

KEA DHCP config is pushed via the Rakefile in the `kea/` subdirectory, not directly by Ansible.
