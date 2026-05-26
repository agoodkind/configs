---
name: deploy-playbook
description: >-
  Deploy Ansible playbooks from the configs repo. Use when the user asks to run,
  dry-run, syntax-check, or reason about an Ansible deploy playbook in this
  repository.
disable-model-invocation: true
---

# Deploy Ansible Playbook

The vault password file at `~/.config/ansible/vault.pass` is required. Use
[scripts/ansible_helper.py](../../../scripts/ansible_helper.py) `deploy` to run
playbooks. Do not invoke `ansible`, `ansible-vault`, `ansible-playbook`,
`ansible-inventory`, or `ansible-console` directly.

## Canonical Playbooks

There are five canonical playbooks. Each targets a specific group; environment
differences between production and testbed come from group vars, not playbook
flags.

| Playbook | Target group | Owns |
| --- | --- | --- |
| [ansible/playbooks/deploy-proxmox.yml](../../../ansible/playbooks/deploy-proxmox.yml) | `proxmox_servers` | mwan-ifmgr, mwan-watchdog, cloudflared-oob, package-updater on hypervisors |
| [ansible/playbooks/deploy-mwan.yml](../../../ansible/playbooks/deploy-mwan.yml) | `mwan_servers` | MWAN VM, prod VM 113 on vault |
| [ansible/playbooks/deploy-mwan-failover.yml](../../../ansible/playbooks/deploy-mwan-failover.yml) | `mwan_failover_servers` or `mwan_failover_test_servers` | MWAN failover LXC |
| [ansible/playbooks/deploy-opnsense.yml](../../../ansible/playbooks/deploy-opnsense.yml) | `opnsense_servers` or `opnsense_test_servers` | mwan-opnsense daemon on OPNsense host |
| [ansible/playbooks/deploy-testbed.yml](../../../ansible/playbooks/deploy-testbed.yml) | `suburban_servers` | Suburban-only extras, including `qm args`, ISP LXCs, host bridge, and VFIO |

## Invocation Pattern

```bash
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py deploy <name> [--limit <host>] [--check] [--diff]
```

`<name>` is the playbook stem, such as `deploy-proxmox` or `deploy-mwan`. The
helper resolves it to `playbooks/<name>.yml` under
[ansible/](../../../ansible/). A full `.yml` path is also accepted.

Always include `--limit <host-or-group>` for production runs so one command does
not touch both hypervisors at once. Dry-run with `--check --diff` first when in
doubt.

## Examples

```bash
# Configure both Proxmox hypervisors
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py deploy deploy-proxmox

# Configure only vault
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py deploy deploy-proxmox --limit vault

# Dry-run the MWAN VM playbook
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py deploy deploy-mwan --check --diff

# Configure the testbed failover LXC
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py deploy deploy-mwan-failover --limit mwan_failover_test_servers

# Configure the testbed OPNsense
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py deploy deploy-opnsense --limit opnsense_test_servers

# Suburban-only testbed extras
python3 /Users/agoodkind/Sites/configs/scripts/ansible_helper.py deploy deploy-testbed --limit suburban
```

## Rake Shortcuts

The [ansible/Rakefile](../../../ansible/Rakefile) wraps the same invocations:

```bash
rake help
rake deploy:proxmox[vault]
rake check:mwan
rake syntax:all
rake inventory
```

## Notes

- The helper sets `cwd` to [ansible/](../../../ansible/) so `ansible.cfg` and
  dynamic inventory resolve. The rake tasks do the same.
- The vault password file is `~/.config/ansible/vault.pass`.
- For a single deploy of a non-MWAN service, such as proxy, adguard, ddns, or
  tack, pass the matching `deploy-<service>` stem to the helper.

## Debugging Failures

When a playbook fails, investigate the root cause before adding workarounds:

1. If a variable is missing, trace where it should come from, such as inventory,
   `set_fact`, or `hostvars`.
2. If a variable name is wrong, check dynamic inventory names, such as
   `proxmox_type` versus `proxmox_vmtype`.
3. If validation fails, check whether the validation logic itself is broken.
4. If a task is skipped unexpectedly, check whether a `when` condition is too
   restrictive.

Do not add `| default()` or `when: var is defined` without first understanding
why the variable is missing.
