---
description: Ansible playbook quality checks and common pitfalls
glob: **/*.yml
alwaysApply: true
---

# Ansible Quality Checks

## Debugging: Fix Root Causes, Not Symptoms

When a variable is missing or a validation fails, investigate **why** before adding defensive code.

### Anti-pattern: Bandaid Defaults and Defensive `when`

- **Problem**: Adding `| default('')` or `when: var is defined` without understanding why the variable is missing masks the real issue.
- **Principle**: `when` is for **logic branches** (deciding between actions), not defensive programming.
- **Symptoms**:
  - Validation tasks failing for values that should exist
  - Variables unexpectedly empty despite being "set"
  - Playbooks silently skipping tasks that should run
- **Root Cause Investigation**:
  1. **Trace the variable source**: Is it from inventory, `set_fact`, `register`, or hostvars?
  2. **Check variable naming**: Proxmox plugin provides `proxmox_type`, not `proxmox_vmtype`. Dynamic inventories have specific variable names.
  3. **Check play/task ordering**: Variables set in one play aren't automatically available in another without `hostvars`.
  4. **Check inventory composition**: Does `proxmox.yml` compose the variable you expect? (e.g., `ansible_proxmox_vmid: proxmox_vmid`)
- **Example**:

  ```yaml
  # ❌ BAD - bandaid that hides the real problem
  type: "{{ proxmox_vmtype | default('lxc') }}"

  # ❌ BAD - defensive when that masks missing data
  - name: Configure service
    ansible.builtin.template:
      src: config.j2
      dest: /etc/service/config
    when: service_config is defined  # Why would it not be defined?

  # ✅ GOOD - when for actual logic branches
  - name: Configure IPv6
    ansible.builtin.template:
      src: ipv6.j2
      dest: /etc/network/ipv6
    when: enable_ipv6 | bool  # Intentional feature flag

  # ✅ GOOD - fix at the source
  # If proxmox_vmtype is missing, add it to proxmox.yml composition:
  #   compose:
  #     proxmox_vmtype: proxmox_type
  ```

- **Checklist before adding `| default()` or `is defined`**:
  1. Where is this variable supposed to come from?
  2. Is it a naming mismatch (e.g., `proxmox_type` vs `proxmox_vmtype`)?
  3. Is it missing from inventory composition?
  4. Is it set in a different play that hasn't run yet?
  5. Only add defensive defaults after confirming the source is correct

## Line Length Limits

- **Prefer**: Stay below 80 columns for readability
- **Acceptable**: Up to 90 columns is okay
- **Hard Limit**: 120 columns is the absolute maximum - use only when unavoidable
- **Check**: All YAML files should respect these limits
- **Fix**: Break long lines using:
  - YAML block scalars (`>-`, `|`) with proper Jinja2 whitespace control (`{{-`, `-}}`)
  - Jinja2 string concatenation
  - Variable extraction for repeated long strings
- **Example**:

  ```yaml
  # ❌ BAD - exceeds 120 columns
  url: "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node }}/lxc/{{ container_result.vmid }}/interfaces"

  # ❌ BAD - `>-` converts newline to space, creating `/lxc/ {{-` with unwanted space
  url: >-
    "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node }}/lxc/
    {{- container_result.vmid }}/interfaces"

  # ✅ GOOD - uses `-}}` to strip trailing whitespace and `{{-` on same line
  url: >-
    "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node -}}/lxc/{{-
    container_result.vmid }}/interfaces"
  ```

## YAML Formatting Issues

### Folded Block Scalars (`>-`) with URLs

- **Problem**: YAML `>-` folded block scalars replace newlines with spaces, breaking URLs split across lines.
- **Check**: Any `url:` field using `>-` with multi-line URLs.
- **Fix**:
  - For simple URLs: Use single-line quoted strings
  - For long URLs that must be broken: Use `-}}` on the previous variable to strip trailing whitespace, and place `{{-` on the same line as the preceding text to avoid newline spaces
- **Example**:

  ```yaml
  # ❌ BAD - inserts space in URL
  url: >-
    https://github.com/user/repo/releases/download/
    v{{ version }}/file.tar.gz

  # ✅ GOOD - single line
  url: "https://github.com/user/repo/releases/download/v{{ version }}/file.tar.gz"

  # ✅ GOOD - broken line with proper Jinja2 whitespace control
  url: >-
    "https://{{ host }}/api/nodes/{{ node -}}/lxc/{{-
    vmid }}/interfaces"
  ```

### Quoted Strings Inside Folded Scalars (breaks `uri` URLs)

- **Problem**: When using `>-`, surrounding the value in quotes makes the quotes part of the string. This can produce errors like `unknown url type: "https` when used with `ansible.builtin.uri`.
- **Check**:
  - `url: >-` (or `Authorization: >-`) where the folded block contains a leading `"` or trailing `"`
  - Ansible errors containing `unknown url type: \"https` or a URL that begins with `"https`
- **Fix**:
  - If you use `>-`, **do not** add surrounding quotes inside the block
  - Keep whitespace control (`{{-`, `-}}`) when you must wrap long templated values
- **Example**:

  ```yaml
  # ❌ BAD - quotes become literal characters in the URL
  url: >-
    "https://example.com/path"

  # ✅ GOOD - no embedded quotes
  url: >-
    https://example.com/path
  ```

### Blockinfile with ExecStart

- **Problem**: YAML `>-` in `blockinfile` content can be included literally in output files (e.g., systemd service files).
- **Check**: Any `blockinfile` task with `block: |` or `block: >-` containing `ExecStart` or other commands.
- **Fix**: Use `block: |` (literal block) and ensure no `>-` appears in the block content itself.
- **Example**:

  ```yaml
  # ❌ BAD - `>-` appears in output
  block: |
    ExecStart=-/sbin/agetty >-
      --autologin root tty%I 115200 $TERM

  # ✅ GOOD
  block: |
    ExecStart=-/sbin/agetty --autologin root tty%I 115200 $TERM
  ```

### YAML Operators Inside `shell:` Blocks (become literal arguments)

- **Problem**: YAML block scalar operators like `>-` are **only** meaningful in YAML. If you put `>-` inside a `shell: |` command block, it is passed to the program as a **literal argument** and can cause confusing runtime failures (e.g., `exit status 2` from `htpasswd`).
- **Check**:
  - Any `shell: |` / `ansible.builtin.shell: |` block that contains `>-` or `|` as part of the command text
  - Especially when used as “line continuation” for commands (e.g., `htpasswd -nb >- "{{ user }}" ...`)
- **Fix**:
  - Use YAML formatting for YAML (`url: >-`, `Authorization: >-`), not inside shell command text
  - Prefer `ansible.builtin.command` with `argv:` (or a dedicated Ansible module) instead of `shell`
  - If you truly need multi-line shell, use shell line continuations (`\`) or a heredoc
- **Example**:

  ```yaml
  # ❌ BAD - `>-` is a literal shell argument
  - ansible.builtin.shell: |
      htpasswd -nb >-
        "{{ user }}" >-
        "{{ pass }}" >-
        /etc/traefik/auth/dashboard-users

  # ✅ GOOD - command argv, output captured and written via copy
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

- **Fact**: systemd-networkd does NOT merge duplicate sections. Only the first `[Section]` is processed.
- **Check**: Any `blockinfile` that might create duplicate sections (e.g., `[DHCPv6]`).
- **Fix**: Use `lineinfile` with `regexp` and `insertafter` to add lines to existing sections, or ensure section doesn't exist before using `blockinfile`.

## Variable Safety

### Undefined Variable References

- **Problem**: Referencing variables in `set_fact` that are defined in the same task fails (variables are set concurrently).
- **Check**: Any `set_fact` task where one variable references another variable defined in the same task.
- **Fix**: Split into multiple sequential `set_fact` tasks.
- **Common gotcha**: “Pipeline” facts like `filtered_ipv6` → `container_primary_ip` must be split; later keys in the same `set_fact` cannot see earlier keys.
- **Example**:

  ```yaml
  # ❌ BAD - variables set concurrently
  - set_fact:
      var1: "value"
      var2: "{{ var1 }}-suffix" # var1 not available yet

  # ❌ BAD - derived facts in the same task
  - set_fact:
      filtered_ipv6: "{{ ipv6_candidates | reject('match','^fe80:') | list }}"
      primary_ip: "{{ filtered_ipv6[0] if (filtered_ipv6 | length > 0) else '' }}"

  # ✅ GOOD
  - set_fact:
      var1: "value"
  - set_fact:
      var2: "{{ var1 }}-suffix"

  # ✅ GOOD
  - set_fact:
      filtered_ipv6: "{{ ipv6_candidates | reject('match','^fe80:') | list }}"
  - set_fact:
      primary_ip: "{{ filtered_ipv6[0] if (filtered_ipv6 | length > 0) else '' }}"
  ```

### Jinja2 Filter Safety

- **Problem**: Using `.first` filter on empty lists causes "No first item, sequence was empty" error.
- **Check**: Any Jinja2 expression using `.first` without checking list length.
- **Fix**: Use array indexing with length checks: `(list | length > 0) and list[0]` or `list[0] if (list | length > 0) else default`.
- **Example**:

  ```yaml
  # ❌ BAD - fails on empty list
  ip: "{{ ip_list | first }}"

  # ✅ GOOD
  ip: "{{ ip_list[0] if (ip_list | length > 0) else '' }}"
  ```

## KEA DHCP Configuration

### Mutual Exclusivity

- **DHCPv4**: `hw-address` and `client-id` are mutually exclusive in reservations.
- **DHCPv6**: `hw-address` and `duid` are mutually exclusive in reservations.
- **Check**: Any reservation with both identifiers.
- **Fix**: Use only one identifier per reservation. Prefer `hw-address` when MAC is pinned.

## Linting and Verification

### Ansible-lint Timing

- **Important**: `ansible-lint` is NOT instant - wait 2-5 seconds after making changes before checking linter output
- The linter needs time to analyze YAML syntax, Jinja2 templates, and Ansible module usage
- If linter errors appear immediately after a fix, wait a few seconds and re-check

## Pre-commit Checklist

Before committing Ansible changes, verify:

1. ✅ Line length: prefer <80, acceptable ≤90, hard limit ≤120 columns
2. ✅ No `url:` fields use `>-` with multi-line URLs (unless using proper Jinja2 whitespace control: `-}}` and `{{-` on same line)
3. ✅ No `url: >-` / `Authorization: >-` values contain surrounding quotes (`"..."`)
4. ✅ No `shell: |` blocks contain YAML operators like `>-` used as “line continuation”
5. ✅ No `blockinfile` blocks contain literal `>-` in command output
6. ✅ No `set_fact` tasks reference variables defined in the same task
7. ✅ No `.first` filter used without empty list checks
8. ✅ No KEA reservations with mutually exclusive identifiers
9. ✅ No `blockinfile` creating duplicate systemd-networkd sections (use `lineinfile` instead)
