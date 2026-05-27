# configs

Infrastructure configuration management for `home.goodkind.io`.

Start with [AGENTS.md](AGENTS.md) for repo rules and workflow, then use
[docs/infra/overview.md](docs/infra/overview.md) for the current infrastructure
snapshot and [docs/ansible/overview.md](docs/ansible/overview.md) for Ansible
execution details.

KEA DHCP config is pushed through the Rakefile in [kea/](kea/), not directly by
Ansible.