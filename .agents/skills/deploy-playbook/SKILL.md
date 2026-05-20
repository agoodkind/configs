---
name: deploy-playbook
description: >-
  Deploy Ansible playbooks from the configs repo. Use when the user asks to run,
  dry-run, syntax-check, or reason about an Ansible deploy playbook in this
  repository.
disable-model-invocation: true
---

# Deploy Ansible Playbook

Run locally from [ansible/](../../../ansible/). The vault password file at
`~/.config/ansible/vault.pass` is required.

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

## Direct Invocation Pattern

Use this pattern from the agent terminal so `ansible.cfg` and dynamic inventory
resolve from [ansible/](../../../ansible/):

```bash
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxmox.yml --limit vault"
```

Always include `--limit <host-or-group>` for production runs so one command does
not touch both hypervisors at once. Dry-run with `--check --diff` first when in
doubt.

## Examples

```bash
# Configure both Proxmox hypervisors
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxmox.yml"

# Configure only vault
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-proxmox.yml --limit vault"

# Dry-run the MWAN VM playbook
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-mwan.yml --check --diff"

# Configure the testbed failover LXC
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-mwan-failover.yml --limit mwan_failover_test_servers"

# Configure the testbed OPNsense
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-opnsense.yml --limit opnsense_test_servers"

# Suburban-only testbed extras
bash -c "cd /Users/agoodkind/Sites/configs/ansible && ansible-playbook --vault-password-file ~/.config/ansible/vault.pass playbooks/deploy-testbed.yml --limit suburban"
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

- The command must run from [ansible/](../../../ansible/), where `ansible.cfg` lives.
- The vault password file is `~/.config/ansible/vault.pass`.
- For a single deploy of a non-MWAN service, such as proxy, adguard, ddns, or
  tack, use the matching `deploy-<service>.yml` directly with the same vault
  password flag.

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
