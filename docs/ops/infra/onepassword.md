# Provision service vaults in 1Password

## Install the provider

Use OpenTofu 1.11 or later and the
[v0.1.0 release](https://github.com/agoodkind/onepassword-admin/releases/tag/v0.1.0)
from agoodkind/onepassword-admin. Choose the archive for the operating system
and architecture of the machine that runs OpenTofu.

1. Set `OPADMIN_PLATFORM` to the matching platform, such as `darwin_arm64` or
   `linux_amd64`. Set `OPADMIN_ARCHIVE` to the absolute path of
   `terraform-provider-opadmin_0.1.0_<os>_<arch>.zip` downloaded from that release.
2. Download `terraform-provider-opadmin_0.1.0_SHA256SUMS` and its `.sig` file
   from the release. Import the fork's
   [release-signing-key.asc](https://github.com/agoodkind/onepassword-admin/blob/v0.1.0/release-signing-key.asc)
   with `gpg --import`. Run `gpg --verify` with the signature file and then the
   checksum file as its arguments. Require a valid signature from that key.
3. Run `shasum -a 256 "${OPADMIN_ARCHIVE:?Set OPADMIN_ARCHIVE}"` and compare
   its digest with the archive's entry in the verified checksum file. Continue
   only when the digests match.
4. Extract only the executable into the existing provider mirror. Run these
   commands from the repository root:

   ```sh
   mkdir -p "opentofu/terraform.d/plugins/registry.opentofu.org/agoodkind/opadmin/0.1.0/${OPADMIN_PLATFORM:?Set OPADMIN_PLATFORM}"
   unzip "${OPADMIN_ARCHIVE:?Set OPADMIN_ARCHIVE}" terraform-provider-opadmin_v0.1.0 -d "opentofu/terraform.d/plugins/registry.opentofu.org/agoodkind/opadmin/0.1.0/${OPADMIN_PLATFORM}"
   ```

## Supply credentials

1. Supply the existing R2 credentials and state encryption passphrase through
   `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and
   `TF_VAR_vault_tofu_state_passphrase`. Follow the credential and passphrase
   requirements in [OpenTofu state operations](../../../opentofu/backend.md).
   Keep their values out of command arguments and repository files.
2. Supply a 1Password service account token with permission to create vaults
   through `OP_SERVICE_ACCOUNT_TOKEN`. Use this token only for provisioning;
   application deployments must use their own credentials.

## Review and apply the plan

1. Edit the service and environment selections in the
   [provisioning root](../../../opentofu/shared/onepassword-vaults/main.tf).
   Each selected pair creates a separate vault. Changing either identifier
   changes its resource address.
   OpenTofu blocks vault deletion and replacement with `prevent_destroy`.
2. Initialize the root with the local provider mirror:

   ```sh
   tofu -chdir=opentofu/shared/onepassword-vaults init -input=false -plugin-dir=../../terraform.d/plugins
   ```

3. Save and inspect the encrypted plan:

   ```sh
   tofu -chdir=opentofu/shared/onepassword-vaults plan -input=false -out=vaults.tfplan
   tofu -chdir=opentofu/shared/onepassword-vaults show vaults.tfplan
   ```

4. Apply the reviewed plan. This command creates the planned vaults in the
   1Password account associated with the provisioning token:

   ```sh
   tofu -chdir=opentofu/shared/onepassword-vaults apply vaults.tfplan
   tofu -chdir=opentofu/shared/onepassword-vaults output -json vault_ids
   ```

   `vault_ids` maps each service and environment to its vault ID.
