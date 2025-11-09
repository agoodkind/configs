# Configuration Projects

Consolidated repository for configuration management projects.

## Structure

```
configs/
├── Gemfile              # Shared Ruby dependencies
├── Gemfile.lock         # Generated lock file
├── Rakefile             # Parent Rakefile - manages all projects
├── Makefile             # Shared Makefile
├── LICENSE              # Apache 2.0 License
├── .rubocop.yml         # Shared RuboCop configuration
├── .gitignore           # Shared Git ignore patterns
├── bind-kea/            # Kea DHCP and BIND DNS configuration
│   ├── Rakefile         # Project-specific tasks
│   └── kea/             # Configuration files
└── logstash/            # Logstash pipeline configuration
    ├── Rakefile         # Project-specific tasks
    ├── conf/            # Logstash configuration files
    ├── ruby/            # Ruby filter scripts
    └── spec/            # RSpec tests
```

## Quick Start

Install dependencies:

```bash
rake install
```

View available commands:

```bash
rake help
```

## Common Commands

### Setup

- `rake install` - Install Ruby dependencies

### Project Management

- `rake list` - List all projects and their tasks
- `rake deploy_all` - Deploy all projects
- `rake test_all` - Test all projects
- `rake status_all` - Show status of all projects
- `rake backup_all` - Backup all projects
- `rake clean_all` - Clean all projects

### Project-Specific

- `rake bind_kea:deploy` - Deploy Kea/BIND configuration
- `rake logstash:deploy` - Deploy Logstash configuration
- `rake <project>:tasks` - List all tasks for a project
- `rake <project>:invoke[task_name]` - Run arbitrary task in project

## Deployment Modes

### Local

Run tasks directly on the local system.

### Remote (SSH)

Deploy to a remote host via SSH:

```bash
REMOTE=1 rake deploy_all
REMOTE=1 REMOTE_HOST=user@host rake logstash:deploy
```

### Proxmox Container

Deploy to a Proxmox LXC container:

```bash
PROXMOX_HOST=root@pve PROXMOX_VMID=100 rake deploy_all
```

## Development

### Testing

Run tests for projects with test suites:

```bash
rake test_all
```

### Linting

RuboCop configuration is shared across all projects:

```bash
cd bind-kea && bundle exec rubocop
cd logstash && bundle exec rubocop
```

### Formatting

Auto-format code:

```bash
cd <project> && bundle exec rubocop -a
```

## Projects

### bind-kea

Kea DHCP and BIND DNS configuration management for router and Proxmox container.

Tasks:
- Deploy Kea DHCP4/DHCP6 configurations to router
- Deploy Kea DHCP-DDNS to Proxmox container
- Manage BIND DNS records

### logstash

Logstash pipeline configuration for firewall log parsing and enrichment.

Tasks:
- Deploy Ruby filter scripts
- Deploy pipeline configurations
- Run diagnostics and tests
- Monitor logs

## License

Apache License 2.0

