# Ansible quality checks

## Default values policy

Every input value is declared explicitly: in the service's group_vars, in
[ansible/inventory/group_vars/all/service_mapping.yml](../../../ansible/inventory/group_vars/all/service_mapping.yml),
or in OpenTofu. A playbook or template reads it bare and fails at load time when
it is missing. Do not use `| default(...)` or `is defined` on an input variable
to paper over a missing value or to infer presence. When you need a branch, drive
`when:` from an explicit flag, not from whether a variable happens to be set.

Inferring a value from whether it was set is banned in every form: `| default(...)`,
`is defined`, `.get(key, default)` (a default in disguise), any `| length`
comparison (an "is this set" or "how big" check in disguise), and
`lookup(..., default=...)` (an env-presence default in disguise; the place the
declared value stops being deterministic). The policy does not allow automatic
exceptions or escape hatches, and the linter lists every banned line, not one per repeated
snippet. The only defensible reason to read a
value defensively is a command result from an outside service that is unset more
often than not, and that judgment is the author's to make and defend in review.

The linter parses each Jinja expression with a Go engine and resolves the operand
each default or presence construct reads. A few Ansible-Jinja forms the Go engine
cannot parse, such as a parenthesized conditional piped into a filter, are routed
to a jinja2 reference parser, whose resolved violations are enforced the same as
any other. The router runs as a
`python3` subprocess, so `configsctl lint` requires `python3` with the `jinja2`
package on PATH. A form neither parser can read is listed for review.

## KEA DHCP reservations

- DHCPv4: `hw-address` and `client-id` are mutually exclusive.
- DHCPv6: `hw-address` and `duid` are mutually exclusive.

Use one identifier per reservation. Prefer `hw-address` when the MAC is pinned.
