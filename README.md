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
├── .env.example         # Example environment variables
├── lib/                 # Shared Rake utilities
│   ├── rake_common.rb   # Common deployment functions
│   └── README.md        # Library documentation
├── ansible/             # Ansible configuration management
│   ├── Rakefile         # Project-specific tasks
│   ├── playbooks/       # Ansible playbooks
│   └── roles/           # Ansible roles
├── bind-kea/            # Kea DHCP and BIND DNS configuration
│   ├── Rakefile         # Project-specific tasks
│   └── kea/             # Configuration files
├── logstash/            # Logstash pipeline configuration
│   ├── Rakefile         # Project-specific tasks
│   ├── conf/            # Logstash configuration files
│   ├── ruby/            # Ruby filter scripts
│   └── spec/            # RSpec tests
└── traefik/             # Traefik reverse proxy configuration
    ├── Rakefile         # Project-specific tasks
    ├── traefik.yml      # Static configuration
    └── dynamic/         # Dynamic routing configuration
```

## Quick Start

1. Copy `.env.example` to `.env` and configure your hosts:

```bash
cp .env.example .env
# Edit .env with your actual host values
```

2. Install dependencies:

```bash
rake install
```

3. View available commands:

```bash
rake help
```

**Note:** The `.env` file in the configs/ directory is automatically loaded by all subdirectory Rakefiles.

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
LOGSTASH_HOST=root@logstash.example.com rake logstash:deploy
KEA_HOST=root@kea.example.com rake bind_kea:kea:deploy
ANSIBLE_HOST=root@ansible.example.com rake ansible:deploy
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

Kea DHCP and BIND DNS configuration management for router and remote Kea/BIND host.

Tasks:
- Deploy Kea DHCP4/DHCP6 configurations to router
- Deploy Kea DHCP-DDNS to remote host
- Manage BIND DNS records

### logstash

Logstash pipeline configuration for firewall log parsing and enrichment.

Tasks:
- Deploy Ruby filter scripts
- Deploy pipeline configurations
- Run diagnostics and tests
- Monitor logs

### traefik

Traefik reverse proxy configuration for public service access.

Tasks:
- Manage routing rules for `*.public.home.goodkind.io`
- Deploy SSL/TLS certificates via Let's Encrypt
- Configure security middlewares
- Update configurations via Git workflow

See [traefik/README.md](traefik/README.md) and [traefik/WORKFLOW.md](traefik/WORKFLOW.md) for detailed documentation.

## License

Apache License 2.0
