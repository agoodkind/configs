# Validate configuration

Run these checks from the repository root.

Install OpenTofu 1.13.2, TFLint 0.66.0, and the Go version in [.go-version](../../../.go-version). Native lint checks source and managed guest references without vault or R2 credentials. Repeated literal findings remain advisory.

1. Check native formatting and provider-free ownership inputs:

   ```bash
   ./configsctl lint
   ```

2. Initialize the provider-free artifact policy module:

   ```bash
   tofu -chdir=opentofu/modules/overlay/host-artifacts init -backend=false -input=false
   ```

3. Run its native policy tests:

   ```bash
   tofu -chdir=opentofu/modules/overlay/host-artifacts test
   ```

These checks evaluate configuration and rendered output without changing a host or infrastructure state.

## Configure retirement verification

The retirement workflow reads state with dedicated CI credentials when a change removes a retirement instruction. Repository and environment secrets currently do not provide these credentials. The verifier rejects affected cleanup until an authorized operator configures them. The verifier skips state access when the proposal preserves retirement instructions.

1. Create dedicated R2 credentials with object-read and bucket-list access for the state bucket and workspace prefixes. Do not grant object writes or deletes.
2. Add repository secrets `RETIREMENT_R2_ACCESS_KEY_ID` and `RETIREMENT_R2_SECRET_ACCESS_KEY` from those dedicated credentials.
3. Add `RETIREMENT_STATE_PASSPHRASE` with the state decryption input. Do not copy operator credentials or passphrases into GitHub.
4. Run the workflow against a real retirement cleanup and an intentional failing cleanup before requiring the `retirement` status in the repository ruleset.

The workflow installs an immutable reviewed verifier before exposing state credentials. It fetches proposed Git objects without checking out proposed code. The verifier reads backend and encryption configuration from the trusted base revision. A separate job publishes the verdict against the reviewed head SHA after confirming that the pull request still has the same base and head.

## KEA DHCP reservations

- DHCPv4: `hw-address` and `client-id` are mutually exclusive.
- DHCPv6: `hw-address` and `duid` are mutually exclusive.

Use one identifier per reservation. Prefer `hw-address` when the MAC is pinned.
