# configs

Infrastructure configuration management for `goodkind.io`.

For detailed infrastructure context, SSH reference, deployment workflows, and LLM guidelines,
see [AGENTS.md](AGENTS.md).

## Directory overview

```
configs/
├── AGENTS.md               # Infrastructure snapshot, SSH reference, LLM guidelines
├── Rakefile                # Parent Rakefile
├── Makefile                # Shared make targets
├── Gemfile / Gemfile.lock  # Shared Ruby dependencies
├── lib/                    # Shared Rake utilities (rake_common.rb)
├── ansible/                # Ansible playbooks, roles, and inventory
├── bind/                   # BIND named.conf.options.j2 (used by deploy-dns64.yml)
├── common/                 # Shared systemd units deployed to all guests via prep-guests.yml
├── consul/                 # Consul service discovery config
├── kea/                    # KEA DHCP4/DHCP6 config + Rakefile deploy tool
├── logstash/               # Logstash pipeline (retired; no live instance)
├── mwan/                   # Multi-WAN VM config, scripts, and docs
├── nanomdm/                # NanoMDM enrollment profile (not yet deployed)
├── proxmox/                # Static Proxmox watchdog files (superseded by mwan/proxmox/)
├── sshpiper/               # SSHPiper config template (deployed via deploy-proxy.yml)
├── traefik/                # Traefik static and dynamic config templates
└── ups-nut/                # NUT UPS operations guide (not yet Ansible-managed)
```

## Quick start

Install Ruby dependencies:

```bash
bundle install
```

Deploy a playbook (run from the `ansible/` directory or the Ansible container at `::107`):

```bash
ansible-playbook playbooks/prep-guests.yml
```

Push KEA DHCP config:

```bash
cd kea && rake deploy
```
