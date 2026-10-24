# Operations

These pages are for running and recovering the homelab.

Production hosts serve live traffic for people who cannot recover from a bad
change on their own. Read the live host before you trust repo templates. State a
hypothesis, test the smallest reversible change, verify that it does not cause a
regression, and only then codify the change in git. Do not bulk-change MWAN, OPNsense, or the vault
hypervisor, and do not restart networking services without a rollback path.

Run OpenTofu and Ansible through configsctl from the repository root. The
[configsctl](../../configsctl) script there runs the configsctl release this
checkout pins, so continuous integration, the pre-commit hook, and every operator
run the same version. To move to a newer release, change the version in that
script.

```bash
./configsctl tofu apply
./configsctl deploy <name> [--limit <host>] [--check] [--diff]
```

A play that installs the MWAN binary or opnsensectl pulls the release its
environment pins onto the controller first. It downloads each archive against
the pinned checksum, verifies the GitHub attestation, and unpacks it under the
ignored `.make/releases` directory. No release is named on the command line,
and a check run stages the release without touching a host. After it copies
the binary onto a Linux host, the play runs `mwan version` on that copy and
fails unless the reported commit is a prefix of the release's commit.
`deploy-proxmox` and `deploy-opnsense` check opnsensectl the same way. To move
an environment to a newer release, set its tag, the archive checksums from
that release's `checksums.txt`, and for MWAN the repository that publishes the
release in
[mwan_prod_all.yml](../../ansible/inventory/group_vars/mwan_prod_all.yml) or
[mwan_testbed_all.yml](../../ansible/inventory/group_vars/mwan_testbed_all.yml).
Use `--limit` on production so one command does not touch both hypervisors.
Dry-run with `--check --diff` before a mutating run.

## Manage Cloudflare tunnel DNS

Use account credentials in the Mac and Vault `cloudflared` management
certificates for tunnel management and private IP routes. The
`cloudflared tunnel route dns` command calls a legacy endpoint that does not
accept these credentials. Use
the standard DNS record API for manual DNS changes.

Manage permanent tunnel CNAMEs through `cloudflare_dns_record.static` in the
[DNS declarations](../../opentofu/cloudflare_dns_records.tf). Review and apply
those changes through `./configsctl tofu`.

For a manual change outside those declarations, use an account token with
`DNS Write` permission restricted to the required zone. Keep its value in a
protected file rather than command arguments.

1. [List DNS records](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/list/)
   for the exact hostname in that zone.
2. If the hostname does not have a record,
   [create a DNS record](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/)
   with `type: CNAME`, `content: <tunnel UUID>.cfargotunnel.com`, `proxied: true`,
   and `ttl: 1`.
3. If an existing record needs an explicit overwrite,
   [update that DNS record](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/edit/).
   Preserve its unrelated fields.
4. Read the record again to confirm its target and proxy setting.

## Run logs

Both commands write their output to a file in the host temp directory, under a
`configs-runs` directory, and print that path before the run starts. The output
arrives over minutes and is the record you need when a run goes wrong, so it
goes somewhere nothing can truncate, buffer, or pipe it away. Follow a run with
`tail -f <path>`. Secret values are redacted in the file exactly as they are on
the terminal, and each run gets its own file, so an earlier run's record
survives.

The logs sit outside the repository, so a run leaves the working tree clean and
the host reclaims old logs on its own schedule. Copy a log you need to keep
before the host clears it.

A run refuses to start when something other than its own private directory sits
at that path, and the error names both the path and the reason. On a host where
the temp directory is shared, another user can create that name first. Remove
the path it names, then run the command again.

A `tofu apply` or `tofu destroy` without `-auto-approve` keeps the terminal
instead, because you cannot answer an approval prompt you cannot see. The same
holds for every other tofu subcommand; only `plan`, `refresh`, and an
auto-approved `apply` or `destroy` write to a file.

- [MWAN](mwan/layout.md)
- [OPNsense](opnsense/operations.md)
- [Deploy and restore Kea configuration](opnsense/kea.md)
- [Ansible](ansible/quality.md)
- [Infrastructure](infra/access.md)
- [Configure PowerEdge NIC firmware](infra/firmware.md)
