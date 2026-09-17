# Application files belong to the binary; site files belong to configs

The gateway and router deploys copy application files out of whichever checkout runs the play. A deploy's result therefore depends on the checkout, not on the release. On 2026-09-16 a deploying checkout carried an off-pin `third_party/yang` submodule and validated a production deploy against schema files the release never saw. This design removes that class of failure by giving every file one home, decided by one rule.

Links point at commit `8693b22f` of `agoodkind/configs`, `7aa0177e` of `agoodkind/opnsensectl`, and `6795d9c6` of `YangModels/yang`, so the references stay true as files move.

## The rule

Would the file change if the same binary were deployed to a different site, meaning another network with its own ISPs, interface names, addresses, and keys?

- **Never.** The file belongs to the program. It lives in the binary's repository as a template, the binary embeds it with `go:embed`, and the binary's install verb writes it onto the host and applies it.
- **Always.** The file belongs to the site. `configs` renders it from inventory, as today.
- **Mixed.** It is the program's file with a few site values in it. It lives in the binary's repository as a template, and the install verb renders it from the values `configs` already hands the program in `config.toml` and `network.json`.

The testbed and production are two such sites already. A file identical on both belongs to the program.

One refinement for mixed files: when the file's values can change while the daemon runs (a provider is added or its link identity changes in `network.json`), the daemon renders it at runtime rather than the install verb rendering it at deploy time. The per-provider networkd units are that case.

## Where each file lands

| File | Changes per site? | Home |
|---|---|---|
| [`mwan-agent.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-agent.service), [`mwan-ifmgr@.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-ifmgr%40.service), [`mwan-ifmgr.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-ifmgr.service), [`mwan-trace-boot.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/services/mwan-trace-boot.service) | never | `mwan` binary |
| The eight YANG schema modules: [`ietf-yang-types`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-yang-types%402025-12-22.yang), [`ietf-inet-types`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-inet-types%402025-12-22.yang), [`iana-if-type`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/iana-if-type%402014-05-08.yang), [`ietf-interfaces`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-interfaces%402018-02-20.yang), [`ietf-ip`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-ip%402018-02-22.yang), [`ietf-routing`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-routing%402018-03-13.yang), [`ietf-nat`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-nat%402019-01-10.yang), and [`goodkind-mwan-steering`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/yang/goodkind-mwan-steering%402026-09-14.yang) | never, and the model must match the binary | `mwan` binary |
| [`nacm-anonymous.xml`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/wanconfig/nacm-anonymous.xml), the read-only RESTCONF policy | never | `mwan` binary |
| [`rousette.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/wanconfig/rousette.service), [`nghttpx-wanconfig.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/wanconfig/nghttpx-wanconfig.service) | never | `mwan` binary |
| [`99-quiet-console.conf`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/99-quiet-console.conf), the [`nftables`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/overrides/nftables.service.d-override.conf) and [`systemd-networkd`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/overrides/systemd-networkd.service.d-override.conf) service overrides | never | `mwan` binary |
| [`sysctl-mwan.conf.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/sysctl-mwan.conf.j2) (interface names), [`rt_tables.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/rt_tables.j2) (provider tables), [`nghttpx-wanconfig.conf.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/wanconfig/nghttpx-wanconfig.conf.j2) (management address, RESTCONF port) | mixed | `mwan` binary, rendered from `config.toml` and `network.json` |
| `config.toml`, rendered from [`config-vm.toml.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/config-vm.toml.j2) and [`config-host.toml.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/config-host.toml.j2) | always | `configs` |
| `network.json`, rendered from [`network.json.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/network.json.j2) | always | `configs` |
| The 802.1X chain: [`wpa_supplicant.conf.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/wpa_supplicant.conf.j2), the [scripts](https://github.com/agoodkind/configs/tree/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/scripts) and [services](https://github.com/agoodkind/configs/tree/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/services), certificates from vault | always | `configs`, by ruling |
| The console getty drop-ins: [`getty@tty1`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/overrides/getty%40tty1.service.d-override.conf), [`serial-getty@ttyS0`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/overrides/serial-getty%40ttyS0.service.d-override.conf) | always, host infrastructure | `configs`, by ruling |
| The per-provider [networkd unit files](https://github.com/agoodkind/configs/tree/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/networkd) | mixed; every value comes from the provider entry in `network.json` | the `mwan` daemon, at runtime: `mwan-ifmgr@wan` renders them from its loaded network configuration at startup and on reload, writes only on change, and asks networkd to reload (`MWAN-491` under `MWAN-324`). Neither the install verb nor the deploy writes them. `MWAN-397` later retires the rendered units when the daemon owns bring-up. |
| [`nftables.conf.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/nftables.conf.j2) | mixed, and deleted by `MWAN-341` when the daemon programs the firewall directly | `configs` until then; not moved |
| The pinned-destination refresher: [`update-att-pinned-dests.sh`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/scripts/update-att-pinned-dests.sh), its [service](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/services/mwan-update-att-pinned-dests.service) and [timer](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/timers/mwan-update-att-pinned-dests.timer), and [`wpa-wait-att-iface.sh`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/scripts/wpa-wait-att-iface.sh) | never, but deleted by `MWAN-387` to `MWAN-389` when the daemon refreshes the sets itself | `configs` until then; not moved |
| The router [`rc.d` script](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense), [run shim](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/opnsense-src/usr/local/libexec/mwan-opnsense-run), the [rest of `opnsense-src`](https://github.com/agoodkind/configs/tree/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/opnsense-src), and the two hypervisor host units [`mwan-opnsense-host.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-opnsense-host.service) and [`mwan-opnsense-drain.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-opnsense-drain.service) | never | `opnsensectl` binary |
| The opnsensectl config, rendered from [`opnsense/config.toml.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/opnsense/config.toml.j2) | always | `configs` |

The wanconfig stack itself, seven Debian packages the `mwan` release builds, stays a release archive beside the binary; it is not a file the program writes. Its two units and the nghttpx config are the program's files and move.

Nothing else under `third_party` is deployed. Once the schema is embedded, the submodule serves only the schema gates in `mwan`'s own tests and `Makefile`.

## The install verb

`mwan install` with no flags prints its help and exits without touching the host. `mwan install --apply` runs on the host as root after the binary is in place. It reads `config.toml` and `network.json`, renders every embedded template, writes each file to its path only when the content differs, and then applies what it wrote through library calls, not shell scripts:

- systemd units and drop-ins: `daemon-reload`, then enable, through the systemd D-Bus API already in the module's dependencies.
- sysctl: each key written through the sysctl runner the module already has.
- The schema and the RESTCONF policy: install or update each module into sysrepo and import the policy, through the sysrepo binding the module already links.
- `rt_tables`: written; nothing to apply.

The verb prints one line per file that changed and exits non-zero on any failure. Running it twice changes nothing the second time. It never restarts the daemon that runs it; the playbook keeps the restart decision.

`opnsensectl install` has the same contract, help by default and `--apply` to act, on the router (`rc.d` script, run shim, `rc.conf` defaults, loader entry) and on the hypervisor (the two host units), using the same idempotent write and the platform's service manager.

The templates are files in each repository, embedded with `go:embed`. They are not Go string literals. A reviewer reads a unit file as a unit file.

## What the release contains

- `mwan` release: the `mwan` binary archive and the wanconfig stack archive, as today. No schema archive; the schema is inside the binary.
- `opnsensectl` release: the two binary archives, as today.

## What the playbooks do

The playbook installs the binary from the staged release, renders `config.toml` and `network.json` from inventory, runs `mwan install --apply`, and decides restarts. It copies no application file from the checkout. The `yanglint` validation on the controller reads the schema the binary carries: `mwan install --print-schema <dir>` writes the embedded modules to a directory on the controller, so the validation and the gateway install read the same bytes.

Release staging itself moves out of `configsctl` and back into the playbooks under `MWAN-490`: each deploy pulls the pinned release with the pattern [`deploy-clyde.yml`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/ansible/playbooks/deploy-clyde.yml#L46-L63) already uses (`get_url` with a sha256 pinned in [group_vars](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/ansible/inventory/group_vars/clyde_suburban_servers.yml#L5-L9), then `unarchive`), plus `gh attestation verify --owner agoodkind` on each archive. Pins are committed in `group_vars`. `configsctl` keeps only lint, validation, and safe running.

## What this does not change

- `nftables.conf` stays rendered by `configs` until `MWAN-341` replaces it with in-process behavior. Moving it first would move a file that is about to be deleted.
- The per-provider networkd units are the daemon's at runtime under `MWAN-491`; this design adds nothing to that and the install verb does not touch them.
- The 802.1X chain and the console drop-ins stay in `configs` by the goal's ruling.
- Routes, rules, and the served wanconfig tree are unchanged by any slice of this design; each cutover is proven against a capture taken before it.
