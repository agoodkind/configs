# AGENTS

Implement deployments through OpenTofu using the existing shared providers and primitives. When a capability is missing, implement it at the appropriate generic boundary. Do not restore Ansible deployment code or use Ansible as a fallback.

This repository does not contain a Go module. Deployments install the `mwan`
and `opnsensectl` binaries from the pinned releases of
[agoodkind/mwan](https://github.com/agoodkind/mwan) and
[agoodkind/opnsensectl](https://github.com/agoodkind/opnsensectl). Read
[docs/README.md](docs/README.md) for area pages and operator runbooks.

run OpenTofu as `./configsctl tofu`.
Do not invoke `ansible`, `ansible-vault`, `ansible-playbook`,
`ansible-inventory`, or `ansible-console` directly, do not pipe decrypted vault
contents into chat.

## Implementation

- Start from evidence by reading the code and the relevant local docs before
  editing.
- Keep platform-specific behavior behind the relevant boundary.
- Implement the real runtime path without fallback-only code, lint suppressions,
  dummy logs, or compile-only tests.
- Keep types tight and reuse existing types that already own the data.
- Add tests that cover the actual regression or contract.
- Preserve unrelated user changes, and do not revert work you did not make.
- Verify with the real project gates when the change warrants it, and state what
  ran and what did not.
- Report what changed, what was verified, and what residual risk remains.
