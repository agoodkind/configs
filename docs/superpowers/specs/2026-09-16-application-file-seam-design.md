# Application files belong to the binary; site files belong to configs

The gateway and router deploys copy application files out of whichever checkout runs the play. A deploy's result therefore depends on the checkout, not on the release. On 2026-09-16 a deploying checkout carried an off-pin third_party/yang submodule and validated a production deploy against schema files the release never saw. This design removes that class of failure by giving every file one home, decided by one rule.

## The rule

Would the file change if the same binary were deployed to a different site, meaning another network with its own ISPs, interface names, addresses, and keys?

- **Never.** The file belongs to the program. It lives in the binary's repository as a template, the binary embeds it, and the binary's install verb writes it onto the host and applies it.
- **Always.** The file belongs to the site. configs renders it from inventory, as today.
- **Mixed.** It is the program's file with a few site values in it. It lives in the binary's repository as a template, and the install verb renders it from the values configs already hands the program in config.toml and network.json.

The testbed and production are two such sites already. A file identical on both belongs to the program.

## Where each file lands

| File | Changes per site? | Home |
|---|---|---|
| mwan-agent.service, mwan-ifmgr@.service, mwan-ifmgr.service, mwan-trace-boot.service | never | mwan binary |
| The eight YANG schema modules (seven IETF modules plus goodkind-mwan-steering) | never, and the model must match the binary | mwan binary |
| nacm-anonymous.xml, the read-only RESTCONF policy | never | mwan binary |
| rousette.service, nghttpx-wanconfig.service | never | mwan binary |
| 99-quiet-console.conf, the nftables and networkd service overrides | never | mwan binary |
| sysctl-mwan.conf (interface names), rt_tables (provider tables), nghttpx-wanconfig.conf (management address, RESTCONF port) | mixed | mwan binary, rendered from config.toml and network.json |
| config.toml, rendered from config-vm.toml.j2 and config-host.toml.j2 | always | configs |
| network.json, the provider model | always | configs |
| The 802.1X chain: wpa_supplicant config, scripts, units, certificates from vault | always | configs, by ruling |
| The console getty drop-ins | always, host infrastructure | configs, by ruling |
| The networkd files and nftables.conf | mixed, and deleted by MWAN-341 and MWAN-397 when the daemon programs the kernel directly | configs until then; not moved |
| The router rc.d script, run shim, rc.conf defaults, loader entry, and the two hypervisor host units | never | opnsensectl binary |
| The opnsensectl config file | always | configs |

The wanconfig stack itself, seven Debian packages the mwan release builds, stays a release archive beside the binary; it is not a file the program writes. Its two units and the nghttpx config are the program's files and move.

Nothing else under third_party is deployed. Once the schema is embedded, the submodule serves only the schema gates in mwan's own tests and Makefile.

## The install verb

`mwan install` runs on the host as root after the binary is in place. It reads config.toml and network.json, renders every embedded template, writes each file to its path only when the content differs, and then applies what it wrote through library calls, not shell scripts:

- systemd units and drop-ins: daemon-reload, then enable, through the systemd D-Bus API already in the module's dependencies.
- sysctl: each key written through the sysctl runner the module already has.
- The schema and the RESTCONF policy: install or update each module into sysrepo and import the policy, through the sysrepo binding the module already links.
- rt_tables: written; nothing to apply.

The verb prints one line per file that changed and exits non-zero on any failure. Running it twice changes nothing the second time. It never restarts the daemon that runs it; the playbook keeps the restart decision.

`opnsensectl install` has the same contract on the router (rc.d script, run shim, rc.conf defaults, loader entry) and on the hypervisor (the two host units), using the same idempotent write and the platform's service manager.

The templates are files in each repository, embedded with go:embed. They are not Go string literals. A reviewer reads a unit file as a unit file.

## What the release contains

- mwan release: the mwan binary archive and the wanconfig stack archive, as today. No schema archive; the schema is inside the binary.
- opnsensectl release: the two binary archives, as today.

## What the playbooks do

The playbook installs the binary from the staged release, renders config.toml and network.json from inventory, runs the install verb, and decides restarts. It copies no application file from the checkout. The yanglint validation on the controller reads the schema the binary carries: `mwan install --print-schema <dir>` writes the embedded modules to a directory on the controller, so the validation and the gateway install read the same bytes.

Release staging itself moves out of configsctl and back into the playbooks under MWAN-490: each deploy pulls the pinned release with the pattern deploy-clyde.yml already uses (get_url with a sha256 pinned in group_vars, then unarchive), plus `gh attestation verify` on each archive. Pins are committed in group_vars. configsctl keeps only lint, validation, and safe running.

## What this does not change

- The networkd files and nftables.conf stay rendered by configs until MWAN-341 and MWAN-397 replace them with in-process behavior. Moving them first would move files that are about to be deleted.
- The 802.1X chain and the console drop-ins stay in configs by the goal's ruling.
- Routes, rules, and the served wanconfig tree are unchanged by any slice of this design; each cutover is proven against a capture taken before it.
