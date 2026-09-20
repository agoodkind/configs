# Application files belong to the binary; site files belong to configs

Links point at commit `8693b22f` of `agoodkind/configs` and `6795d9c6` of `YangModels/yang`.

> **Historical.** The gateway Go module moved to agoodkind/mwan in configs commit 6f18c39d. The `mwan/go`, `mwan/yang` and `third_party/yang` paths on this page refer to the tree as it was.

## The rule

Would the file change if the same binary were deployed to a different site, meaning another network with its own ISPs, interface names, addresses, and keys?

- **Never.** The file is static. It lives in the binary's repository, the binary embeds it with `go:embed`, and the install verb writes it onto the host and applies it.
- **Otherwise.** The file is templated. It lives in `configs`, which renders it from inventory. One site value is enough.

The testbed and production are two such sites already. A file identical on both belongs to the program.

There is no third category. A templated file stays in `configs` whether its values come from inventory, `config.toml`, or `network.json`.

One exception: the daemon renders the per-provider networkd units at runtime under `MWAN-491`, because their values change while the daemon runs.

## Where each file lands

| File | Changes per site? | Home |
|---|---|---|
| [`mwan-agent.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-agent.service), [`mwan-ifmgr@.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-ifmgr%40.service), [`mwan-ifmgr.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-ifmgr.service), [`mwan-trace-boot.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/services/mwan-trace-boot.service) | never | `mwan` binary |
| The eight YANG schema modules: [`ietf-yang-types`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-yang-types%402025-12-22.yang), [`ietf-inet-types`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-inet-types%402025-12-22.yang), [`iana-if-type`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/iana-if-type%402014-05-08.yang), [`ietf-interfaces`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-interfaces%402018-02-20.yang), [`ietf-ip`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-ip%402018-02-22.yang), [`ietf-routing`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-routing%402018-03-13.yang), [`ietf-nat`](https://github.com/YangModels/yang/blob/6795d9c680bc77fe706b3be9557d8adf12901dcc/standard/ietf/RFC/ietf-nat%402019-01-10.yang), and [`goodkind-mwan-steering`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/yang/goodkind-mwan-steering%402026-09-14.yang) | never, and the model must match the binary | `mwan` binary |
| [`nacm-anonymous.xml`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/wanconfig/nacm-anonymous.xml), the read-only RESTCONF policy | never | `mwan` binary |
| [`rousette.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/wanconfig/rousette.service), [`nghttpx-wanconfig.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/wanconfig/nghttpx-wanconfig.service) | never | `mwan` binary |
| [`99-quiet-console.conf`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/99-quiet-console.conf), the [`nftables`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/overrides/nftables.service.d-override.conf) and [`systemd-networkd`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/overrides/systemd-networkd.service.d-override.conf) service overrides | never | `mwan` binary |
| [`sysctl-mwan.conf.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/sysctl-mwan.conf.j2) (interface names), [`rt_tables.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/rt_tables.j2) (provider tables), [`nghttpx-wanconfig.conf.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/wanconfig/nghttpx-wanconfig.conf.j2) (management address, RESTCONF port) | templated | `configs` |
| `config.toml`, rendered from [`config-vm.toml.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/config-vm.toml.j2) and [`config-host.toml.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/config-host.toml.j2) | always | `configs` |
| `network.json`, rendered from [`network.json.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/network.json.j2) | always | `configs` |
| The 802.1X chain: [`wpa_supplicant.conf.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/wpa_supplicant.conf.j2), the [scripts](https://github.com/agoodkind/configs/tree/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/scripts) and [services](https://github.com/agoodkind/configs/tree/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/services), certificates from vault | always | `configs`, by ruling |
| The console getty drop-ins: [`getty@tty1`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/overrides/getty%40tty1.service.d-override.conf), [`serial-getty@ttyS0`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/overrides/serial-getty%40ttyS0.service.d-override.conf) | always, host infrastructure | `configs`, by ruling |
| The per-provider [networkd unit files](https://github.com/agoodkind/configs/tree/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/networkd) | templated; every value comes from the provider entry in `network.json` | the `mwan` daemon, at runtime under `MWAN-491`. `MWAN-397` retires the rendered units when the daemon owns bring-up. |
| [`nftables.conf.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/config/nftables.conf.j2) | templated; deleted by `MWAN-341` when the daemon programs the firewall directly | `configs` |
| The pinned-destination refresher: [`update-att-pinned-dests.sh`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/scripts/update-att-pinned-dests.sh), its [service](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/services/mwan-update-att-pinned-dests.service) and [timer](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/timers/mwan-update-att-pinned-dests.timer), and [`wpa-wait-att-iface.sh`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/scripts/wpa-wait-att-iface.sh) | never; deleted by `MWAN-387` to `MWAN-389` when the daemon refreshes the sets itself | `configs` |
| The router [`rc.d` script](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/opnsense-src/etc/rc.d/mwan_opnsense), [run shim](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/opnsense-src/usr/local/libexec/mwan-opnsense-run), the [rest of `opnsense-src`](https://github.com/agoodkind/configs/tree/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/opnsense-src), and the two hypervisor host units [`mwan-opnsense-host.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-opnsense-host.service) and [`mwan-opnsense-drain.service`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/mwan/go/cmd/mwan/mwan-opnsense-drain.service) | never | `opnsensectl` binary |
| The opnsensectl config, rendered from [`opnsense/config.toml.j2`](https://github.com/agoodkind/configs/blob/8693b22f892bc745fa16631e4b1fc874fb9ee53e/opnsense/config.toml.j2) | always | `configs` |

The wanconfig stack, seven Debian packages the `mwan` release builds, stays a release archive beside the binary. Its two units and the nghttpx config are separate files with their own rows above.

Nothing else under `third_party` is deployed. Once the schema is embedded, the submodule serves only the schema gates in `mwan`'s own tests and `Makefile`.

## The install verb

`mwan install` with no flags prints its help and exits without touching the host. `mwan install --apply` runs on the host as root after the binary is in place. It writes each embedded file to its path only when the content differs, then applies what it wrote through library calls:

- systemd units and drop-ins: `daemon-reload`, then enable, through the systemd D-Bus API.
- The quiet-console sysctl file: each key written through the sysctl runner.
- The schema and the RESTCONF policy: install or update each module into sysrepo and import the policy, through the sysrepo binding.

The verb renders no template and reads no site value. Every file it writes is byte-identical at every site.

The verb prints one line per file that changed and exits non-zero on any failure. Running it twice changes nothing the second time. It never restarts the daemon that runs it; the playbook keeps the restart decision.

`opnsensectl install` has the same contract on the router (`rc.d` script, run shim, `rc.conf` defaults, loader entry) and on the hypervisor (the two host units).

The embedded files are files in each repository, embedded with `go:embed`, not Go string literals.

## What the release contains

- `mwan` release: the `mwan` binary archive and the wanconfig stack archive. No schema archive; the schema is inside the binary.
- `opnsensectl` release: the two binary archives.

## What the playbooks do

The playbook installs the binary from the staged release, renders every templated file from inventory, runs `mwan install --apply`, and decides restarts. It copies no static application file from the checkout. The `yanglint` validation on the controller reads the schema the binary carries: `mwan install --print-schema <dir>` writes the embedded modules to a directory on the controller, so the validation and the gateway install read the same bytes.

Release staging lives in the playbooks under `MWAN-490`: each deploy pulls the pinned release with `get_url` against a sha256 pinned in `group_vars`, then `unarchive`, then `gh attestation verify --owner agoodkind` on each archive. `configsctl` keeps only lint, validation, and safe running.
