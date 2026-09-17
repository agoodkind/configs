# Plan: one home per application file

Design: [Application files belong to the binary; site files belong to configs](../specs/2026-09-16-application-file-seam-design.md).

Order follows the repo split goal: MWAN-490 first, then the mwan install verb under MWAN-305, then opnsensectl under MWAN-410. Each slice is one reviewed pull request, deployed to the testbed from main and validated live before the next, then to production on explicit approval per command. Every slice keeps routes, rules, and the served wanconfig tree byte-identical to a capture taken before it.

## Slice 1. Playbooks pull and verify the releases (MWAN-490)

1. Add `ansible/playbooks/tasks/github-release.yml`, run on the controller, driven by vars: `release_repo`, `release_tag`, `release_assets` (a list of `{name, checksum}`), `release_stage_root`. Per asset: `get_url` from the release download URL with `checksum: sha256:<declared>`, then `gh attestation verify <file> --owner agoodkind`, then `unarchive` into `<stage>/<tag>/<name without platform suffix>/`. Resolve the tag to its full commit with `gh api repos/<repo>/commits/<tag>`. Set the facts the plays read today: `mwan_release_tag`, `mwan_release_commit`, `mwan_release_dir`, `wanconfig_stack_dir`, `opnsensectl_release_tag`, `opnsensectl_release_commit`, `opnsensectl_release_dir`. Drop `wanconfig_stack_manifest`; nothing reads it.
2. Pin in group_vars: `mwan_release_tag` and `mwan_release_assets` for the gateway sources, `opnsensectl_release_tag` and `opnsensectl_release_assets` for the router and hypervisor sources, following group_vars/clyde_suburban_servers.yml.
3. Import the task in deploy-mwan, deploy-mwan-failover, deploy-testbed, deploy-proxmox, deploy-opnsense. The existing copy tasks keep their paths (`<dir>/linux_amd64/mwan`, `<stack dir>/debs/`).
4. Land while configsctl still accepts `--release`; run deploys without the flag so the play's own facts are used. Prove on the testbed: deploy-mwan, deploy-proxmox, deploy-opnsense from main with no release flag; the verify tasks assert the pinned commits. Then production.

## Slice 2. Authorized keys back to deploy-ssh-keys.yml (MWAN-490)

1. Replace the `go run goodkind.io/configsctl/cmd/deploy-authorized-keys` task with the original shape: fetch `https://github.com/{{ github_ssh_keys_user }}.keys` with `uri`, strip, dedupe, and sort the lines in `set_fact`, append the sshpiper restricted key for the second bundle, write both bundle paths with `copy` and `content:`, `assert` the list is not empty. Remove the configsctl version lookup.
2. Prove on the testbed hypervisor and one guest: the deployed authorized_keys equals the previous bundle byte for byte.

## Slice 3. configsctl loses staging and authorized keys (MWAN-490)

1. In configsctl, delete internal/release, internal/authorizedkeys, cmd/deploy-authorized-keys, the `--release` and `--opnsensectl-release` flags, their tests, and the README lines. The command list becomes lint, validation, and safe running.
2. Bump the configsctl pin in configs to that release; update the Rakefile bypass and every doc that shows `--release`.
3. Prove: every deploy playbook runs from main on the testbed with the new pin; then production.

## Slice 4. mwan install verb, static files (MWAN-390, 391, 392, 393 to 396)

1. In mwan/go, embed the four mwan units, the schema modules, nacm-anonymous.xml, rousette.service, nghttpx-wanconfig.service, 99-quiet-console.conf, and the two service overrides. Move each file from its configs path into the module; the schema modules move from third_party/yang and mwan/yang into the module's embedded tree, and the Makefile's schema gates and the Go tests read the embedded copies.
2. Add `mwan install`: write each file when content differs, daemon-reload and enable units through the systemd D-Bus API, install or update the schema and import the policy through the sysrepo binding, print one line per changed file, exit non-zero on failure, idempotent on the second run. Add `mwan install --print-schema <dir>` for the controller-side validation.
3. deploy-mwan and the stack task stop copying those files and run `mwan install` after installing the binary; the yanglint validation reads the printed schema directory. Remove the third_party/yang submodule from configs once nothing reads it.
4. Prove on the testbed: a deploy from a checkout with no submodule initialized succeeds; installed files hash-equal the embedded copies; a reboot converges; capture comparison unchanged. Then production.

## Slice 5. mwan install verb, rendered files (MWAN-382 to 386)

1. Embed sysctl-mwan.conf, rt_tables, and nghttpx-wanconfig.conf as templates rendered from the loaded config.toml and network.json. Add `restconf_port` under `[wanconfig]` in config.toml, the one value those templates need that config.toml does not carry yet.
2. `mwan install` renders and writes them and applies sysctl through the sysctl runner.
3. deploy-mwan stops templating them. Prove as in slice 4.

## Slice 6. opnsensectl install verb (MWAN-410)

1. In opnsensectl, embed the rc.d script, run shim, rc.conf defaults, loader entry, and the two host units. Add `opnsensectl install` with the same contract for the router and the hypervisor.
2. mwan-opnsense-deploy.yml and mwan-opnsense-host-deploy.yml stop copying those files and run the verb. Prove on the testbed router and hypervisor: the router contract holds (install path, symlink, rc.d supervision, run shim, argv0 fast path, daemon version over the channel). Then production, one host per session.

## Verification that closes the plan

- deploy-mwan, deploy-proxmox, deploy-opnsense, deploy-testbed, and deploy-mwan-failover run from main on the testbed and production with no `--release` flag, no configsctl staging, and no application file copied from the checkout except network.json, config.toml, the 802.1X chain, the console drop-ins, and, until MWAN-341 and MWAN-397, the networkd files and nftables.conf.
- configsctl's command list is lint, validation, and safe running.
- configs holds no third_party/yang submodule.
- A deploy from a fresh shallow clone with no submodule produces a gateway byte-identical, in routes, rules, and served tree, to one deployed from a full checkout.
