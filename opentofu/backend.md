# State backend

OpenTofu state lives in the Cloudflare R2 bucket named tofu-state, outside the
homelab, so losing a hypervisor or a workstation never loses the state. The
backend speaks the S3 protocol with native lockfile locking, so concurrent runs
block each other instead of corrupting state.

Run every tofu command through the repo control tool so credentials flow from
the Ansible vault automatically:

```bash
go run goodkind.io/configs/cmd/configs tofu plan
go run goodkind.io/configs/cmd/configs tofu apply
```

The wrapper reads four vault secrets and exports them for the child process:
the R2 access pair (`vault_r2_tofu_access_key_id`,
`vault_r2_tofu_secret_access_key`) authenticates the backend, and the two
Proxmox token secrets become the provider token variables. Nothing needs a
`terraform.tfvars` file, and no secret is exported by hand. A fresh checkout
needs one `configs tofu init` before its first plan.

To rotate the backend credential, mint a new Cloudflare API token with the
Workers R2 Storage Write permission, derive the S3 pair (the access key id is
the token id, the secret is the SHA-256 hex of the token value), and feed both
names to `configs set-secrets`. The old token can then be revoked in the
Cloudflare dashboard.

The backend declaration itself lives in [backend.tf](backend.tf); its endpoint
embeds the Cloudflare account id, which is stable and not a secret.
