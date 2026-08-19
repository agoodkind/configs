# Operations

These pages are for running and recovering the homelab.

Production hosts serve live traffic for people who cannot recover from a bad
change on their own. Read the live host before you trust repo templates. State a
hypothesis, test the smallest reversible change, verify no regression, and only
then codify the change in git. Do not bulk-change MWAN, OPNsense, or the vault
hypervisor, and do not restart networking services without a rollback path.

Run OpenTofu and Ansible through the configs binary.

```bash
go run goodkind.io/configs/cmd/configs tofu apply
go run goodkind.io/configs/cmd/configs deploy <name> [--release <tag>] [--limit <host>] [--check] [--diff]
```

A play that installs the MWAN binary fails at load without `--release`. Use
`--limit` on production so one command does not touch both hypervisors. Dry-run
with `--check --diff` before a mutating run.

- [MWAN](mwan/layout.md)
- [OPNsense](opnsense/operations.md)
- [Ansible](ansible/quality.md)
- [Infrastructure](infra/access.md)
