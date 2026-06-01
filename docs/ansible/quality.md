# Ansible quality checks

Style and safety rules for Ansible playbooks, roles, and templates in this repo.
See [docs/ansible/overview.md](overview.md) for inventory layout and
[docs/ansible/secrets.md](secrets.md) for the vault contract.

## Debugging: fix root causes, not symptoms

When a variable is missing or a validation fails, investigate why before adding
defensive code.

`when:` is for logic branches (deciding between actions), not defensive
programming. Adding `| default('')` or `when: var is defined` without
understanding why the variable is missing masks the real issue.

Symptoms that mean you are looking at a root-cause bug, not a missing default:

- Validation tasks fail for values that should exist.
- Variables are unexpectedly empty despite being "set".
- Playbooks silently skip tasks that should run.

Trace the variable source before patching:

1. Where is it supposed to come from (inventory, `set_fact`, `register`,
   hostvars)?
2. Is there a naming mismatch? The Proxmox plugin emits `proxmox_type`, not
   `proxmox_vmtype`. Dynamic inventories have specific variable names.
3. Are play and task ordering correct? Variables set in one play are not
   automatically available in another without `hostvars`.
4. Is the inventory composition right? See `compose:` in
   [ansible/inventory/vault.proxmox.yml](../../ansible/inventory/vault.proxmox.yml).

```yaml
# Bad - bandaid that hides the real problem
type: "{{ proxmox_vmtype | default('lxc') }}"

# Bad - defensive `when` that masks missing data
- name: Configure service
  ansible.builtin.template:
    src: config.j2
    dest: /etc/service/config
  when: service_config is defined

# Good - `when` for an actual logic branch (feature flag)
- name: Configure IPv6
  ansible.builtin.template:
    src: ipv6.j2
    dest: /etc/network/ipv6
  when: enable_ipv6 | bool

# Good - fix at the source
# If proxmox_vmtype is missing, add it to the proxmox.yml compose block:
#   compose:
#     proxmox_vmtype: proxmox_type
```

Checklist before adding `| default()` or `is defined`:

1. Where is this variable supposed to come from?
2. Is it a naming mismatch?
3. Is it missing from inventory composition?
4. Is it set in a different play that has not run yet?
5. Only add defensive defaults after confirming the source is correct.

## Default values policy

Every input value is declared explicitly: in the service's group_vars, in
[ansible/inventory/group_vars/all/service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml),
or in OpenTofu. A playbook or template reads it bare and fails at load time when
it is missing. Do not use `| default(...)` or `is defined` on an input variable
to paper over a missing value or to infer presence. When you need a branch, drive
`when:` from an explicit flag, not from whether a variable happens to be set.

Inferring a value from whether it was set is banned in every form: `| default(...)`,
`is defined`, `.get(key, default)` (a default in disguise), and any `| length`
comparison (an "is this set" or "how big" check in disguise). There is no
automatic exception and no escape hatch. The only defensible reason to read a
value defensively is a command result from an outside service that is unset more
often than not, and that judgment is the author's to make and defend in review.
Restructure instead: declare the value, gate a task with the module's own
`failed_when` or `changed_when`, or initialize an accumulator with `set_fact`
before the loop.

Enforced by `scripts/lint_ansible_defaults.py`: the ansible helper runs it before
every deploy, the lint path runs it, and pre-commit runs it on staged files. The
check flags every occurrence; it grants no exception, so a genuine outside-service
case is the author's call to defend, not the check's to allow.

## Line length

- Prefer below 80 columns.
- Up to 90 columns is acceptable.
- 120 columns is the hard limit.

Break long lines using YAML block scalars (`>-`, `|`) with explicit Jinja2
whitespace control (`{{-`, `-}}`), Jinja2 concatenation, named variables, or by
splitting conditionals at logical points (`if`, `and`, `or`).

```yaml
# Bad - exceeds 120 columns
url: "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node }}/lxc/{{ container_result.vmid }}/interfaces"

# Bad - `>-` collapses the newline to a space, breaking the URL
url: >-
  "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node }}/lxc/
  {{- container_result.vmid }}/interfaces"

# Good - strip trailing whitespace with `-}}` and put `{{-` on the same line
url: >-
  "https://{{ proxmox_api_host }}:8006/api2/json/nodes/{{ target_node -}}/lxc/{{-
  container_result.vmid }}/interfaces"
```

## YAML formatting traps

### Folded scalars (`>-`) with URLs

YAML `>-` folds newlines to spaces, which breaks URLs split across lines.

```yaml
# Bad - inserts a space inside the URL
url: >-
  https://github.com/user/repo/releases/download/
  v{{ version }}/file.tar.gz

# Good - single line
url: "https://github.com/user/repo/releases/download/v{{ version }}/file.tar.gz"

# Good - broken with explicit whitespace control
url: >-
  "https://{{ host }}/api/nodes/{{ node -}}/lxc/{{-
  vmid }}/interfaces"
```

### Quoted strings inside folded scalars

Quotes inside `>-` become part of the value. `ansible.builtin.uri` will then
fail with `unknown url type: "https`.

```yaml
# Bad - quotes are literal characters in the URL
url: >-
  "https://example.com/path"

# Good - no embedded quotes
url: >-
  https://example.com/path
```

### `blockinfile` with systemd `ExecStart`

`>-` inside a `blockinfile` payload is written to the destination file
literally, which breaks systemd units.

```yaml
# Bad - `>-` lands in the unit file
block: |
  ExecStart=-/sbin/agetty >-
    --autologin root tty%I 115200 $TERM

# Good
block: |
  ExecStart=-/sbin/agetty --autologin root tty%I 115200 $TERM
```

### YAML operators inside `shell:` blocks

YAML operators like `>-` are YAML syntax, not shell syntax. They pass through as
literal arguments to the command and produce confusing runtime errors (for
example, `htpasswd` exiting 2).

```yaml
# Bad - `>-` becomes a literal shell argument
- ansible.builtin.shell: |
    htpasswd -nb >-
      "{{ user }}" >-
      "{{ pass }}" >-
      /etc/traefik/auth/dashboard-users

# Good - structured argv via `command`, then copy the output
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

### systemd-networkd section merging

systemd-networkd does not merge duplicate sections. Only the first `[Section]`
is processed. `blockinfile` that creates a second `[DHCPv6]` silently loses the
new keys. Use `lineinfile` with `regexp` and `insertafter` to extend an existing
section, or guarantee the section does not exist before using `blockinfile`.

## Variable safety

### Undefined references inside the same `set_fact`

`set_fact` evaluates all keys concurrently, so a later key cannot see an
earlier key in the same task.

```yaml
# Bad - var1 is not yet available when var2 evaluates
- ansible.builtin.set_fact:
    var1: "value"
    var2: "{{ var1 }}-suffix"

# Bad - pipeline facts in one task
- ansible.builtin.set_fact:
    filtered_ipv6: "{{ ipv6_candidates | reject('match','^fe80:') | list }}"
    primary_ip: "{{ filtered_ipv6[0] if (filtered_ipv6 | length > 0) else '' }}"

# Good - split into sequential tasks
- ansible.builtin.set_fact:
    var1: "value"
- ansible.builtin.set_fact:
    var2: "{{ var1 }}-suffix"
```

### Jinja2 `.first` on possibly empty lists

`.first` on an empty list raises "No first item, sequence was empty". Use
indexed access with a length check.

```yaml
# Bad
ip: "{{ ip_list | first }}"

# Good
ip: "{{ ip_list[0] if (ip_list | length > 0) else '' }}"
```

## KEA DHCP reservations

- DHCPv4: `hw-address` and `client-id` are mutually exclusive.
- DHCPv6: `hw-address` and `duid` are mutually exclusive.

Use one identifier per reservation. Prefer `hw-address` when the MAC is pinned.

## Secrets

Vault-first. Do not rely on local files on the controller. Store secrets in
[ansible/inventory/group_vars/all/vault.yml](../../ansible/inventory/group_vars/all/vault.yml)
and inject with `content: "{{ vault_var }}"` in `copy` tasks rather than `src:`.
Full contract in [docs/ansible/secrets.md](secrets.md).

## Dynamic list parsing

`uri` returns multi-line strings for list-like data (Cloudflare IP ranges,
etc.). Splitting in YAML breaks if you template the string directly.

```yaml
# Bad - renders as one invalid long string
trusted_ips: "{{ response.content.split('\n') }}"

# Good - a real YAML list
trusted_ips: "{{ response.content.splitlines() }}"
```

## Process management

- For debugging or complex deployments that might race, force sequential
  execution with `--forks=1`.
- Ensure previous `ansible-playbook` processes are killed before starting a new
  run if they might be hung.

## Linting and verification

`ansible-lint` is not instant. Wait a few seconds after edits before checking
linter output. The linter analyses YAML, Jinja2, and module usage, so freshly
edited files may briefly report stale warnings.

## Pre-commit checklist

Before committing Ansible changes, verify:

1. Line length: prefer below 80, acceptable up to 90, hard limit 120.
2. No `url:` fields use `>-` with multi-line URLs unless using explicit Jinja2
   whitespace control.
3. No `url: >-` or `Authorization: >-` values contain surrounding quotes.
4. No `shell: |` blocks contain YAML operators like `>-` used as line
   continuation.
5. No `blockinfile` payload contains literal `>-` in command output.
6. No `set_fact` task references a variable defined in the same task.
7. No `.first` filter used without an empty-list check.
8. No KEA reservation uses mutually exclusive identifiers.
9. No `blockinfile` creates a duplicate systemd-networkd section; use
   `lineinfile` instead.
10. No `| default()` or `is defined` on an input variable. Declare it in
    group_vars and read it bare; both are allowed only on command or register
    output. `scripts/lint_ansible_defaults.py` enforces this.
