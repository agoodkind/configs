# configs

Infrastructure configuration management for `goodkind.io`.

Start with [AGENTS.md](AGENTS.md) for repo rules and workflow, then use
[docs/infra/overview.md](docs/infra/overview.md) for the current infrastructure
snapshot and [docs/ansible/overview.md](docs/ansible/overview.md) for Ansible
execution details.

## Quick start

Dependencies are managed with Bundler. Install them before running Rake tasks.

The intended Ansible controller is the ansible container. It has the repo,
Ansible dependencies, and the vault password file in place. Playbooks can also
be triggered through Semaphore.

KEA DHCP config is pushed through the Rakefile in [kea/](kea/), not directly by
Ansible.
