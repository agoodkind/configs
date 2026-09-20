# AGENTS

This repository provisions guests with OpenTofu and configures them with
Ansible. It holds no Go module, and the deploy installs the `mwan` and
`opnsensectl` binaries from the pinned releases of
[agoodkind/mwan](https://github.com/agoodkind/mwan) and
[agoodkind/opnsensectl](https://github.com/agoodkind/opnsensectl). Read
[docs/README.md](docs/README.md) for area pages and operator runbooks.

Run every deploy as `./configsctl deploy <name>` from the repository root, and
run OpenTofu as `./configsctl tofu`. A rake deploy task runs the same command.
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
