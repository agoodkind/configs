# Validate configuration

Run these checks from the repository root.

Install OpenTofu 1.13.2, TFLint 0.66.0, and the Go version in [.go-version](../../../.go-version). Native lint checks source and managed guest references without vault or R2 credentials. Repeated literal findings remain advisory.

1. Check native formatting and provider-free ownership inputs:

   ```bash
   ./configsctl lint
   ```

2. Install Python's Jinja2 package and check deployment templates:

   ```bash
   ./configsctl lint mwan/config/*.j2 opnsense/*.j2 proxmox/services/*.j2
   ```

These checks evaluate configuration and rendered output without changing a host or infrastructure state.

## Configure retirement verification

The retirement workflow reads state with dedicated CI credentials when a change removes a retirement instruction. The required `retirement` status blocks merges when verification fails. The verifier skips state access when the proposal preserves retirement instructions.

1. Create the GitHub environment `retirement-state`. Restrict deployment branches to the selected branch `main` and exclude tags. Preserve branch protection for `main`.
2. Create dedicated R2 credentials with object-read and bucket-list access for the state bucket and workspace prefixes. Do not grant object writes or deletes.
3. Add environment secrets `RETIREMENT_R2_ACCESS_KEY_ID` and `RETIREMENT_R2_SECRET_ACCESS_KEY` to `retirement-state` from those dedicated credentials. Do not create repository-wide copies.
4. Add `RETIREMENT_STATE_PASSPHRASE` to `retirement-state` with the passphrase that currently decrypts the state, supplied by `vault_tofu_state_passphrase`. Keep this secret restricted to the environment. Use the dedicated R2 credentials from the previous steps for backend access.
5. Run the workflow against a real retirement cleanup and an intentional failing cleanup before requiring the `retirement` status in the repository ruleset.

The workflow accepts pull requests targeting `main` and checks the current base before reading state. Retargeting requests a fresh check. The verification job uses the restricted environment. Ordinary branch push workflows cannot read its secrets.

The workflow installs an immutable reviewed verifier before exposing state credentials. It fetches proposed Git objects without checking out proposed code. The verifier reads backend and encryption configuration from the trusted base revision. A separate job without state secrets publishes the verdict against the reviewed head SHA after confirming that the pull request still targets `main` with the same base and head.

## KEA DHCP reservations

- DHCPv4: `hw-address` and `client-id` are mutually exclusive.
- DHCPv6: `hw-address` and `duid` are mutually exclusive.

Use one identifier per reservation. Prefer `hw-address` when the MAC is pinned.
