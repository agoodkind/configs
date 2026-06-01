---
name: ansible-quality
description: >-
  Run Ansible playbook quality checks and apply common Ansible fixes in the
  configs repo. Use when editing Ansible YAML, reviewing playbooks, or
  debugging Ansible failures.
disable-model-invocation: true
---

# Ansible Quality Checks

## Debugging: Fix Root Causes, Not Symptoms

When a variable is missing or a validation fails, investigate **why** before adding defensive code.

### Anti-pattern: Bandaid Defaults and Defensive `when`

- **Rule (enforced)**: Never use `| default(...)` or `is defined` on an input variable. Declare every input value explicitly in the service's group_vars (or `service_mapping.yml` or OpenTofu) and read it bare; a missing value must fail at load time, not silently default. `default()` and `is defined` are allowed only on module or register **output** (the shape of a command result), never on a config input. `when:` is for logic branches between actions, driven by an explicit flag, not defensive programming.
- **Enforcement**: `scripts/lint_ansible_defaults.py` flags every input-side violation. The ansible helper runs it before each deploy, the lint path runs it, and pre-commit runs it on staged files. There is no per-line escape hatch; a genuine command-result read must take a form the allowlist recognizes, such as a registered name or a result attribute like `.rc`, `.stdout`, or `.stat`.
- **Symptoms**:
  - Validation tasks failing for values that should exist
  - Variables unexpectedly empty despite being "set"
  - Playbooks silently skipping tasks that should run
- **Root Cause Investigation**:
  1. **Trace the variable source**: Is it from inventory, `set_fact`, `register`, or hostvars?
  2. **Check variable naming**: Proxmox plugin provides `proxmox_type`, not `proxmox_vmtype`. Dynamic inventories have specific variable names.
  3. **Check play/task ordering**: Variables set in one play are not automatically available in another without `hostvars`.
  4. **Check inventory composition**: Does the relevant Proxmox plugin file ([ansible/inventory/vault.proxmox.yml](../../../ansible/inventory/vault.proxmox.yml) or [ansible/inventory/suburban.proxmox.yml](../../../ansible/inventory/suburban.proxmox.yml)) compose the variable you expect? For example, `ansible_proxmox_vmid: proxmox_vmid`.
- **Example**:

```yaml
# BAD - bandaid that hides the real problem
type: "{{ proxmox_vmtype | default('lxc') }}"

# BAD - defensive when that masks missing data
- name: Configure service
  ansible.builtin.template:
    src: config.j2
    dest: /etc/service/config
  when: service_config is defined

# GOOD - when for actual logic branches
- name: Configure IPv6
  ansible.builtin.template:
    src: ipv6.j2
    dest: /etc/network/ipv6
  when: enable_ipv6 | bool

# GOOD - fix at the source
# If proxmox_vmtype is missing, add it to the per-hypervisor Proxmox
# plugin file composition ([ansible/inventory/vault.proxmox.yml](../../../ansible/inventory/vault.proxmox.yml)
# or [ansible/inventory/suburban.proxmox.yml](../../../ansible/inventory/suburban.proxmox.yml)):
#   compose:
#     proxmox_vmtype: proxmox_type
```

- **Checklist before adding `| default()` or `is defined`**:
  1. Where is this variable supposed to come from?
  2. Is it a naming mismatch, such as `proxmox_type` versus `proxmox_vmtype`?
  3. Is it missing from inventory composition?
  4. Is it set in a different play that has not run yet?
  5. Only add defensive defaults after confirming the source is correct.

## Line Length Limits

- **Prefer**: Stay below 80 columns for readability.
- **Acceptable**: Up to 90 columns is okay.
- **Hard Limit**: 120 columns is the absolute maximum. Use it only when unavoidable.
- **Check**: All YAML files should respect these limits.
- **Fix**: Break long lines using:
  - YAML block scalars (`>-`, `|`) with proper Jinja2 whitespace control (`{{-`, `-}}`).
  - Jinja2 string concatenation.
  - Variable extraction for repeated long strings.

```yaml
# BAD - exceeds 120 columns
url: "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node }}/lxc/{{ container_result.vmid }}/interfaces"

# BAD - `>-` converts newline to space, creating `/lxc/ {{-` with unwanted space
url: >-
  "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node }}/lxc/
  {{- container_result.vmid }}/interfaces"

# GOOD - uses `-}}` to strip trailing whitespace and `{{-` on same line
url: >-
  "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node -}}/lxc/{{-
  container_result.vmid }}/interfaces"
```

## YAML Formatting Issues

### Folded Block Scalars (`>-`) with URLs

- **Problem**: YAML `>-` folded block scalars replace newlines with spaces, breaking URLs split across lines.
- **Check**: Any `url:` field using `>-` with multi-line URLs.
- **Fix**:
  - For simple URLs, use single-line quoted strings.
  - For long URLs that must be broken, use `-}}` on the previous variable to strip trailing whitespace, and place `{{-` on the same line as the preceding text to avoid newline spaces.

### Quoted Strings Inside Folded Scalars

- **Problem**: When using `>-`, surrounding the value in quotes makes the quotes part of the string. This can produce errors like `unknown url type: "https` when used with `ansible.builtin.uri`.
- **Check**:
  - `url: >-` or `Authorization: >-` where the folded block contains a leading `"` or trailing `"`.
  - Ansible errors containing `unknown url type: \"https` or a URL that begins with `"https`.
- **Fix**:
  - If you use `>-`, do not add surrounding quotes inside the block.
  - Keep whitespace control (`{{-`, `-}}`) when you must wrap long templated values.

### Blockinfile with ExecStart

- **Problem**: YAML `>-` in `blockinfile` content can be included literally in output files, such as systemd service files.
- **Check**: Any `blockinfile` task with `block: |` or `block: >-` containing `ExecStart` or other commands.
- **Fix**: Use `block: |` and ensure no `>-` appears in the block content itself.

### YAML Operators Inside `shell:` Blocks

- **Problem**: YAML block scalar operators like `>-` are only meaningful in YAML. If you put `>-` inside a `shell: |` command block, it is passed to the program as a literal argument.
- **Check**:
  - Any `shell: |` or `ansible.builtin.shell: |` block that contains `>-` or `|` as command text.
  - Shell commands that use `>-` as line continuation, such as `htpasswd -nb >- "{{ user }}" ...`.
- **Fix**:
  - Use YAML formatting for YAML, not inside shell command text.
  - Prefer `ansible.builtin.command` with `argv:` or a dedicated Ansible module.
  - If multi-line shell is required, use shell line continuations (`\`) or a heredoc.

```yaml
# BAD - `>-` is a literal shell argument
- ansible.builtin.shell: |
    htpasswd -nb >-
      "{{ user }}" >-
      "{{ pass }}" >-
      /etc/traefik/auth/dashboard-users

# GOOD - command argv, output captured and written via copy
- ansible.builtin.command:
    argv: ["htpasswd", "-nbB", "{{ user }}", "{{ pass }}"]
  register: htpasswd_out
  changed_when: false
  no_log: true

- ansible.builtin.copy:
    dest: /etc/traefik/auth/dashboard-users
    content: "{{ htpasswd_out.stdout }}\n"
    mode: "0600"
  no_log: true
```

### Systemd-networkd Section Merging

- **Fact**: systemd-networkd does not merge duplicate sections. Only the first `[Section]` is processed.
- **Check**: Any `blockinfile` that might create duplicate sections, such as `[DHCPv6]`.
- **Fix**: Use `lineinfile` with `regexp` and `insertafter`, or ensure the section does not exist before using `blockinfile`.

## Variable Safety

### Undefined Variable References

- **Problem**: Referencing variables in `set_fact` that are defined in the same task fails because variables are set concurrently.
- **Check**: Any `set_fact` task where one variable references another variable defined in the same task.
- **Fix**: Split into multiple sequential `set_fact` tasks.
- **Common gotcha**: Pipeline facts like `filtered_ipv6` to `container_primary_ip` must be split. Later keys in the same `set_fact` cannot see earlier keys.

```yaml
# BAD - variables set concurrently
- set_fact:
    var1: "value"
    var2: "{{ var1 }}-suffix"

# GOOD
- set_fact:
    var1: "value"

- set_fact:
    var2: "{{ var1 }}-suffix"
```

### Jinja2 Filter Safety

- **Problem**: Using `first` on empty lists causes `No first item, sequence was empty`.
- **Check**: Any Jinja2 expression using `first` without checking list length.
- **Fix**: Use array indexing with length checks.

```yaml
# BAD - fails on empty list
ip: "{{ ip_list | first }}"

# GOOD
ip: "{{ ip_list[0] if (ip_list | length > 0) else '' }}"
```

## KEA DHCP Configuration

- DHCPv4: `hw-address` and `client-id` are mutually exclusive in reservations.
- DHCPv6: `hw-address` and `duid` are mutually exclusive in reservations.
- Use only one identifier per reservation. Prefer `hw-address` when MAC is pinned.

## Linting and Verification

### Ansible-lint Timing

- `ansible-lint` is not instant. Wait 2 to 5 seconds after making changes before checking linter output.
- The linter needs time to analyze YAML syntax, Jinja2 templates, and module usage.
- If linter errors appear immediately after a fix, wait a few seconds and re-check.

## Pre-commit Checklist

Before committing Ansible changes, verify:

1. Line length prefers less than 80, accepts 90, and never exceeds 120 columns unless unavoidable.
2. No `url:` fields use `>-` with multi-line URLs unless Jinja2 whitespace control is correct.
3. No `url: >-` or `Authorization: >-` values contain surrounding quotes.
4. No `shell: |` blocks contain YAML operators like `>-` used as line continuation.
5. No `blockinfile` blocks contain literal `>-` in command output.
6. No `set_fact` tasks reference variables defined in the same task.
7. No `first` filter is used without empty-list checks.
8. No KEA reservations use mutually exclusive identifiers.
9. No `blockinfile` creates duplicate systemd-networkd sections. Use `lineinfile` instead.
