# OPNsense 25.7 config-import flow

Target: VM 101 in our suburban testbed, running `OPNsense 25.7 (amd64)` build `25.7-n271606-9af17f0102ca` on `FreeBSD 14.3-RELEASE-p1`. Source citations resolve against `github.com/opnsense/core` tag `25.7` (commit `bbd245bb098cef5a30faa0221238b823c1e49836`, peeled `17d0a22a053233296c5c80d4f8f2e2f349f2ba7d`) and tag `25.7.10` (commit `bb09f97a80aa586de7c8b4b1a069f146a22fa473`, peeled `c2f076f3041d37d59ba219773e5bb34b0a05c7f7`). The bytes I read on VM 101 match the upstream `25.7` tag for the files inspected (`Config.php` lines 645-676, `config.inc:convert_config`, `rc.bootup`, `diag_backup.php` lines 240-372). The build hash `9af17f0102ca` is an OPNsense package-build identifier and does not resolve as a commit on `opnsense/core` (verified via `gh api /repos/opnsense/core/commits/9af17f0102ca` returning HTTP 422). I treat the `25.7` tag as the closest verifiable source and note any drift inline. No verifiable source exists for the exact build commit.

## TL;DR call graph

```
diag_backup.php POST mode=restore
    -> Config::restoreBackup(tmpfile)
        -> Config::overwrite(tmpfile)        # flock(LOCK_EX) + truncate + write + chmod 0640
        -> Config::load()                    # simplexml_load_string, validates only XML well-formedness
    -> parse_config()                        # rebuild $config global
    -> (only if keepconsole or rrddata) write_config() + convert_config()
        -> run_migrations.php                # per-model BaseModel::runMigrations()
        -> pluginctl -i                      # register pluggable interfaces
        -> firmware/register.php sync        # release-type / plugin sync
        -> Config::forceReload(); parse_config()
    -> if is_interface_mismatch(false): cancel reboot, route operator to interfaces_assign.php
    -> if flush_history: configd_run('system flush config_history')
    -> if rebootafterrestore and not mismatched: configd_run('system reboot', true)
        -> /usr/local/etc/rc.reboot -> rc.syshook stop -> shutdown -or now
            -> reboot -> /usr/local/etc/rc -> /usr/local/etc/rc.bootup
                -> convert_config()          # ALWAYS runs on boot
                -> system_devd_configure, system_login_configure
                -> if is_interface_mismatch(): set_networking_interfaces_ports(true)
                                              # interactive console prompt; blocks boot
                -> ordered *_configure() chain (interfaces, filter, plugins, dns, dhcp, ...)
```

The parts that look surprising:

- `restoreBackup()` itself does not migrate the schema. Migration only runs on the `keepconsole` or `rrddata` GUI paths, or via the post-reboot `rc.bootup` call.
- The REST API has no upload-and-replace endpoint at all in core 25.7.
- A `cp /conf/config.xml + reboot` IS the canonical hot path for "load a foreign XML and bring it up", because `rc.bootup` runs the same migration entry point and the same ordered service-reconfigure chain.
- The GUI postpones the reboot if any interface name in the new XML does not exist on the host. On a `cp + reboot`, the same check fires inside `rc.bootup` and drops the boot to an interactive console prompt that reads `php://stdin`.

## 1. Entry points

### 1.1 GUI (System -> Configuration -> Backups)

URL: `https://<host>/diag_backup.php`. Single PHP file at `src/www/diag_backup.php`. Request mode is dispatched by the `restore` POST field (`diag_backup.php:201-202`). Citation: `github.com/opnsense/core` tag `25.7`, file `src/www/diag_backup.php`, lines `200-372`.

Two restore modes share the same form:

- **Full restore**: empty `restorearea[]`. Calls `Config::restoreBackup()` with the uploaded tmpfile (`diag_backup.php:317-318`).
- **Partial restore**: one or more `restorearea[]` values (e.g. `interfaces`, `bridges`, `vlans`, `gifs`, `laggs`, `ppps`, `wireless`, `rrddata`, plus any sections registered via `plugins_xmlrpc_sync()`). Calls `restore_config_section()` (`diag_backup.php:48-151`) which merges the chosen sections into the in-memory `$config`, calls `write_config()`, then `convert_config()` (line `147`). The GUI hides this with a confirmation dialog warning that section restores can break referential integrity (`diag_backup.php:466-490`).

Submit-button name is `restore` (`diag_backup.php:605`). The form posts `multipart/form-data` with file field `conffile`, optional `decrypt`/`decrypt_password` for AES-encrypted backups, and check fields `rebootafterrestore`, `keepconsole`, `flush_history`. Reboot-after-restore is the GET-side default (`diag_backup.php:179`).

ACL coverage: `<page-diagnostics-backup-restore>` matches `diag_backup.php*` (`src/opnsense/mvc/app/models/OPNsense/Core/ACL/ACL.xml`).

### 1.2 REST API

ACL pattern `api/core/backup/*` is registered for `<page-diagnostics-configurationhistory>` (`ACL.xml`). The controller is `BackupController` in `src/opnsense/mvc/app/controllers/OPNsense/Core/Api/BackupController.php` (210 lines on tag `25.7`). Public endpoints, derived directly from `public function *Action` methods:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET`  | `/api/core/backup/providers`                        | list configured backup providers (`This Firewall`, OPNcentral, etc.) |
| `GET`  | `/api/core/backup/backups/{host}`                   | list `config-*.xml` files under the provider directory |
| `GET`  | `/api/core/backup/diff/{host}/{backup1}/{backup2}`  | unified diff of two existing local backups |
| `POST` | `/api/core/backup/deleteBackup/{backup}`            | unlink one local backup file |
| `POST` | `/api/core/backup/revertBackup/{backup}`            | call `Config::restoreBackup()` against an existing local backup file |
| `GET`  | `/api/core/backup/download/{host}[/{backup}]`       | download a backup as `application/octet-stream` |

Citation: `BackupController.php:84-209`. Verified against the live file on VM 101 at `/usr/local/opnsense/mvc/app/controllers/OPNsense/Core/Api/BackupController.php` (210 lines, identical method set).

**There is no upload-and-replace endpoint in core 25.7**. `revertBackup` only accepts a basename of an existing file under `/conf/backup/config-*.xml`. The REST API cannot receive a foreign XML and apply it. A live grep on VM 101 for `upload`, `restoreXmlConfig`, `restore_xml`, `importXml`, `importConfig` under the Core controllers directory returned no matches.

### 1.3 configctl

A live `configctl` enumeration on VM 101 (full output captured in this research) lists no `system restore` or `backup restore` action. The system-related actions are: `remote backup`, `event config_changed`, `flush config_history`, `reboot`, `ha_reconfigure_backup`, `trust configure`, etc. Citation: `/usr/local/opnsense/service/conf/actions.d/actions_system.conf` on VM 101 (full text included in research notes); upstream equivalent at `src/opnsense/service/conf/actions.d/actions_system.conf` tag `25.7`.

`configctl system reboot` is the post-import reboot trigger that `diag_backup.php` itself uses (`configd_run('system reboot', true)` at `diag_backup.php:685`).

### 1.4 Direct file replacement plus reboot

`/conf/config.xml` is the on-disk source of truth. Boot path:

1. `init` runs `/etc/rc autoboot`, which `exec`s `/usr/local/etc/rc` (header at `/etc/rc:3`).
2. `/usr/local/etc/rc` mounts filesystems, recovers users, initializes syshooks and PHP, then `flock`-launches `/usr/local/etc/rc.bootup` (line ~ near the end of `/usr/local/etc/rc`).
3. `rc.bootup` is a PHP script. After requiring core includes, **its first stateful action is `convert_config()`** (`src/etc/rc.bootup:24` on tag `25.7`). The author's comment on the `25.7` tag explicitly states this is for the "import or restore or reset" case.
4. After migration, `rc.bootup` runs an ordered chain of `*_configure()` calls (lines 26-60), then `exit(0)`.

So `cp /conf/config.xml + reboot` IS a supported entry point and triggers the same migration step the GUI calls in the keepconsole/rrddata path. It does NOT trigger the GUI's `flush_history` step, and it does NOT preserve console settings the way `keepconsole=1` does.

## 2. Canonical PHP handler call graph

For the **full-restore** GUI path used by a production-shaped XML import:

1. `is_uploaded_file($_FILES['conffile']['tmp_name'])` (`diag_backup.php:260`) and decrypt branch (`:269-275`).
2. Save current console fields into `$csettings` (`:303-313`).
3. Write decrypted bytes back over the tmpfile (`:316`).
4. `Config::getInstance()->restoreBackup($filename)` (`:318`):
   - If current config is valid, snapshot the in-memory simplexml + filehandle, then `overwrite($filename)` and `load()`. If `load()` throws, restore the snapshot via `save(null, true)` and return false (`Config.php:653-669`).
   - `overwrite()` does `fopen("a+e") + flock(LOCK_EX) + ftruncate(0) + fwrite(file_get_contents($filename))` then `chmod 0640` (`Config.php:632-642`). The new bytes go into `/conf/config.xml` verbatim, with no per-field validation.
   - `load()` does `simplexml_load_string(stream_get_contents($fp))`. Validation is XML well-formedness only (`Config.php:375-410, 416-437`).
5. `parse_config()` (`diag_backup.php:322`): rebuild the global `$config` array from the new XML. Citation: `parse_config()` lives in `src/etc/inc/config.inc` and is heavily used elsewhere; it does not run migrations.
6. If `keepconsole` is set: copy saved console fields back into `$config['system']`, set `$flush = true` (`:323-334`).
7. If `$config['rrddata']` exists: import RRDs, unset that node, set `$flush = true` (`:335-340`).
8. If `$flush`: `write_config()` plus `convert_config()` (`:341-344`).
9. **Migration only runs on the `$flush` branch.** A foreign XML without `keepconsole` and without an `<rrddata>` child does NOT trigger `convert_config()` from this point.
10. `is_interface_mismatch(false)` (`:351`). If true, append "Postponing reboot" and clear `$do_reboot` (`:359-362`).
11. If `flush_history`: `configd_run('system flush config_history')` and a final `write_config()` (`:368-371`).
12. End-of-script: `configd_run('system reboot', true)` (`diag_backup.php:684-685`), which fires `[reboot] -> /usr/local/etc/rc.reboot -> /sbin/shutdown -or now`.

The implicit "transaction" here is only the `restoreBackup()` rollback on parse failure. After parse succeeds, every later step runs forward and any service-side failure is logged but not reverted.

## 3. REST API specifics

`POST /api/core/backup/revertBackup/{backup}`:

- Path parameter is a basename, e.g. `config-1715000000.1234.xml`. Implementation iterates `glob("/conf/backup/config-*.xml")` and matches by basename (`BackupController.php:172-181`).
- Request body: none required. Phalcon's `ApiControllerBase` enforces CSRF / API-key auth at the framework layer. Live behavior on the testbed (untested in this round) is that you can call this with HTTP basic API key + secret.
- Response: JSON `{"status":"reverted"}` on success, `{"status":"not_found"}` on miss.
- It calls `Config::restoreBackup(filename)` and `Config::save()` synchronously. It does NOT call `parse_config()`, `convert_config()`, or any service-reconfigure. It does NOT trigger a reboot.
- Privilege check: `throwReadOnly()` rejects users with `user-config-readonly` ACL (`BackupController.php:169-171, 45-52`).

This means even the REST entry point that exists is a "swap the on-disk XML, leave the live system stale" operation. It assumes a follow-up reboot (or the operator running `convert_config` and the service-reconfigure chain manually).

There is no REST endpoint that accepts an uploaded foreign XML.

## 4. Reboot vs hot-apply

The GUI default is `rebootafterrestore = true` (`diag_backup.php:179`). That default expectation is intentional: nothing in `restoreBackup()` itself reapplies routing, DHCP, firewall, IPsec, OpenVPN, plugins, or interface assignment. The `flush=true` branch only triggers `convert_config()` (which runs migrations), not service reconfigure. The reboot path does both because `rc.bootup` runs the full ordered chain.

If the operator unchecks `rebootafterrestore`, the new XML lives on disk but the kernel and services keep running with the old in-memory state until a manual reload or reboot.

A user-driven hot-apply path would require iterating the same `*_configure()` chain `rc.bootup` runs. There is no single PHP function in `25.7` that does this without a reboot.

## 5. Schema migration

`<config><version>` on VM 101 is currently `v9` (live `grep -E '<version>' /conf/config.xml`). This is the legacy top-level version. The 25.x migration model is per-model: every `BaseModel` subclass under `src/opnsense/mvc/app/models/OPNsense/*/Migrations/M*_*_*.php` is run by `run_migrations.php`. Examples present on VM 101: `OPNsense/Dnsmasq/Migrations/M1_0_0.php`, `OPNsense/IPsec/Migrations/M1_0_0..4.php`, `OPNsense/OpenVPN/Migrations/M1_0_0.php`, `OPNsense/Monit/Migrations/M1_0_0..8.php`, `OPNsense/IDS/Migrations/M1_0_0..7.php`, `OPNsense/Syslog/Migrations/M1_0_2.php`. (`find /usr/local/opnsense -path '*Migration*' -type f` on VM 101.)

`run_migrations.php` (`src/opnsense/mvc/script/run_migrations.php` on tag `25.7`) walks every model, instantiates it, calls `runMigrations()` from `BaseModel`, and reports per-model `Migrated <Class> from <pre> to <post>`. The legacy `<version>` field at the top of `config.xml` is bumped indirectly by `write_config()` writes during migration (the version constant is set in `src/etc/inc/config.lib.inc`'s `latest_config` family of helpers; not inspected here, but visible in older OPNsense source).

When migration runs:

- Always on `rc.bootup` (`src/etc/rc.bootup:24`).
- During GUI restore only on the `keepconsole` or `rrddata` branch (`diag_backup.php:294, 343`). A plain "upload XML, click Restore, leave keepconsole and rrddata both off" does NOT call `convert_config()` from PHP. Migration would still run on the subsequent reboot.
- During `rc.configure_firmware` (`src/etc/rc.configure_firmware:45`).
- It does not run on REST `revertBackup`.

Failure mode: per-model migrations log to the OPNsense system log and print `*** <Class> migration failed from <pre> to <post>, check log for details`. They do not abort the chain.

## 6. Service-reconfigure ordering

The order taken from `rc.bootup` lines 24-60 (tag `25.7`), which is the canonical post-reboot sequence and the one we should verify post-import in the MWAN-153 test matrix:

1. `convert_config()` -> `run_migrations.php`, `pluginctl -i`, `firmware/register.php sync`, `Config::forceReload`, `parse_config()`.
2. `system_devd_configure(true)`.
3. `system_login_configure(true)`.
4. **Interface mismatch gate**: `is_interface_mismatch()` -> `set_networking_interfaces_ports(true)` (interactive console reassignment loop). This blocks the boot until the operator reassigns or every required device exists.
5. `interfaces_loopback_configure(true)`.
6. `system_kernel_configure(true)`.
7. `system_sysctl_configure(true)`.
8. `system_timezone_configure(true)`.
9. `system_firmware_configure(true)`.
10. `system_trust_configure(true)`.
11. `system_hostname_configure(true)`.
12. `system_resolver_configure(true)`.
13. `system_syslog_start(true)`.
14. `filter_configure_sync(true, false)`  (default policy before interfaces).
15. `interfaces_hardware(true)`.
16. `interfaces_configure(true)`.
17. `system_resolver_configure(true)`  (re-run after interfaces).
18. `filter_configure_sync(true)`.
19. `plugins_configure('early', true)`.
20. `system_routing_configure(true, null, 'ignore')`.
21. `plugins_configure('dhcp', true)`.
22. `plugins_configure('dns', true)`.
23. `filter_configure_sync(true)`  (third pass after DHCP/DNS).
24. `plugins_configure('monitor', true)`.
25. `plugins_configure('vpn', true)`.
26. `plugins_configure('bootup', true)`.
27. `system_powerd_configure(true)`.

That is 27 ordered steps with `filter_configure_sync` repeated three times. For MWAN-153, the matrix should at minimum exercise the DHCP, DNS, VPN, monitor, and bootup plugin stages, plus `interfaces_configure` and the routing step.

## 7. Failure modes

- **Malformed XML**. `simplexml_load_string` returns false; `loadFromStream` throws `ConfigException("invalid config xml")` (`Config.php:402-406`). `restoreBackup` catches this, restores the previous in-memory simplexml, calls `save(null, true)` to write the original back to `/conf/config.xml`, and returns false (`Config.php:662-668`). The GUI surfaces `gettext("The configuration could not be restored.")` (`diag_backup.php:347`). Result: no change on disk.
- **Missing or wrong-shape field**. There is NO field-level validation in `restoreBackup`. The GUI accepts any well-formed XML root. Bad fields surface as runtime errors during the boot-time `*_configure()` chain or during later GUI use.
- **Service-reconfigure failure during the GUI flush branch**. `convert_config()` chains use `mwexecf` (non-verbose) which logs but does not abort. The flow continues. There is no rollback at this layer.
- **Reboot-time service failure**. `rc.bootup` does not stop on individual `*_configure()` errors. Each function decides its own logging. A service failure leaves the box running with whatever state earlier steps reached, and the boot still completes. This is a known foot-gun.
- **Interface name does not exist on host** (e.g. `iavf0` referenced but the box only has `vtnet0`). Two cases:
  - Via GUI: `is_interface_mismatch(false)` returns true, GUI cancels the reboot and links to `interfaces_assign.php` (`diag_backup.php:351-363`). The operator can fix it before triggering a reboot manually.
  - Via `cp + reboot`: `rc.bootup:29-32` calls `is_interface_mismatch()` and drops to `set_networking_interfaces_ports(true)` which reads `php://stdin`. On a headless or non-interactive console (vsock, Proxmox vnc, blocked serial), this stalls the boot. This is the fail-mode our prod-shaped XML transform was designed to avoid.
  - The `<lock>` flag inside an `<interfaces>` entry suppresses the mismatch check for that interface (`is_interface_mismatch` in `console.inc:54-83`). I have not verified whether our prod XML uses `<lock>` anywhere; if it does, that interface will not be guarded.

## 8. Comparison: `cp /conf/config.xml + reboot` vs the GUI restore path

| Step | `cp + reboot` | GUI full restore (default) | GUI full restore + `keepconsole=1` |
| --- | --- | --- | --- |
| XML well-formedness check | yes (rc.bootup load) | yes (`Config::load`) | yes |
| Atomic file replace under `flock(LOCK_EX)` | no (plain cp) | yes (`Config::overwrite`) | yes |
| `parse_config()` rebuild of `$config` | yes (rc.bootup) | yes (`diag_backup.php:322`) | yes |
| Console fields preserved | no | no | yes |
| `<rrddata>` import | no | no (unless rrddata sub-tag set) | yes when XML has `<rrddata>` |
| `convert_config()` runs in the import process | n/a | NO | yes |
| `convert_config()` runs at next boot | yes | yes | yes |
| `flush_history` config_history clear | no | optional (`flush_history=1`) | optional |
| `is_interface_mismatch` blocks reboot | no (drops to interactive prompt instead) | yes (postpone reboot, route to GUI) | yes (postpone reboot, route to GUI) |
| Service-reconfigure chain | yes (rc.bootup) | only on next reboot | only on next reboot |
| Audit log entry / revision metadata | no (we did not write through `Config::save`) | yes (Config::save audit) | yes |
| Backup history rotation | no | yes (Config::cleanupBackups via save) | yes |

The functional differences that matter for our case:

- The GUI atomically replaces the on-disk file under `flock(LOCK_EX)`. A naive `cp` is not atomic relative to a concurrent reader. In practice we do `cp` only when nothing else is writing, but it is a real race window. `install -m 0640 newconfig /conf/config.xml` plus `mv -f` would close it.
- The GUI emits an audit log entry through `Config::auditLogChange` and a backup snapshot under `/conf/backup/config-*.xml`. `cp` skips both, so we lose the rollback breadcrumb.
- The GUI guards against a missing-interface boot via the `is_interface_mismatch` check before reboot. `cp + reboot` does not, so a bad transform headlessly jams the boot.
- The GUI does not do any extra service reload beyond what the next reboot does (assuming `keepconsole` and `rrddata` are off). Our `cp + reboot` is therefore equivalent in service-reconfigure terms.
- The GUI does not migrate the schema in-process either (default branch); both paths rely on the next-boot `convert_config()` for that.

## Practical recommendation for config import

Use **direct file replacement + reboot** for automated testbed imports, and adopt three of the GUI's safety practices:

1. **Pre-import interface gate**: before writing the new XML, walk the new `<interfaces>` entries and confirm each `<if>` value resolves to an existing kernel device, treating any mismatch as a hard stop. This replicates the GUI's `is_interface_mismatch` check and prevents the `rc.bootup` interactive-console stall.
2. **Atomic write**: replace `cp` with `install -m 0640 -o root -g wheel <new> /conf/config.xml.new && mv -f /conf/config.xml.new /conf/config.xml`. The mv is atomic on UFS/ZFS within the same filesystem.
3. **Snapshot before swap**: before the mv, copy the current `/conf/config.xml` to `/conf/backup/config-<epoch>.xml`. This recreates the GUI's automatic snapshot and gives the operator a rollback target via `revertBackup` REST or the GUI history page.

Why not the GUI: it requires an authenticated CSRF-bound web form, and `multipart/form-data` upload is awkward to script. We already have an `ssh` channel and write access to `/conf/config.xml` via Ansible.

Why not the REST API: there is no upload-and-replace endpoint in core 25.7, period. `revertBackup` only references a file already under `/conf/backup/`. We could pre-stage our XML there as `config-<epoch>.xml` and call `revertBackup`, but that would skip our own atomic-write guard and still leave us with no service reconfigure (the REST call does not reboot).

Why not `keepconsole=1` semantics: our build is a virtio guest; we control the console settings via Tofu/cloud-init at boot, so we do not need to preserve the running console fields across import. If we ever switch to a hardware appliance with serial console pinning, we should re-evaluate.

VM 101 import checks:

- Capture the `<version>` field before and after import.
- Capture per-model migration log lines on first boot, such as `Migrated OPNsense\Foo\Bar from 1.0.0 to 1.0.1`.
- Treat the first-boot `set_networking_interfaces_ports` interactive prompt as a failed pre-import interface gate.
- Confirm pf rules are stable after the third `filter_configure_sync` pass, which is steps 14, 18, and 23 in the boot sequence above.

## What MWAN-152 rollback design needs to know

- **Schema version drift is a one-way door under default settings.** After a reboot import, per-model migrations have written into `/conf/config.xml`. Restoring an older XML by file-swap is fine on disk, but the next reboot will run `convert_config()` again and the older models will either (a) be migrated forward again to match this build, or (b) fail to upgrade if a downgrade-migration is missing. Test path: stage current XML, file-swap-import the older XML, reboot, observe whether the older fields parse and which models log re-migration.
- **GUI restore writes a backup snapshot; file-swap does not.** If we want symmetry between rollback and forward import, our rollback tool must also write `/conf/backup/config-<epoch>.xml` for every swap. Otherwise an operator looking at the GUI history sees no record of the rollback.
- **`flush_history` is destructive.** If our forward-import tool ever sets the equivalent of `flush_history=1` (we do not today), the operator loses the local history and can no longer revert via the REST endpoint. We should keep `flush_history` off in MWAN-152.
- **`<lock>` semantics.** If any of our prod-shaped interfaces carries `<lock>1</lock>`, both the GUI mismatch check and the boot-time mismatch check skip it. That means a bad interface name on a locked entry will not block the reboot and will only manifest as a service-reconfigure failure later. MWAN-152 should treat any `<lock>` in the XML as "review by hand".
- **The reboot-vs-hot-apply asymmetry**. If MWAN-152 ever wants to roll back without a reboot, it needs to call the same `*_configure()` chain `rc.bootup` runs, in order. There is no single helper for that on `25.7`; expect to call them individually.
- **No transaction across services.** Once `interfaces_configure` runs and the new IPs come up, a later `plugins_configure('vpn')` failure does not roll the interfaces back. The rollback tool must be designed around "apply, observe, decide" rather than "all-or-nothing".

## Sources

Live reads on VM 101 (`OPNsense 25.7 (amd64)` build `25.7-n271606-9af17f0102ca`, FreeBSD 14.3-RELEASE-p1):

- `/conf/config.xml` (`<version>v9</version>`).
- `/usr/local/opnsense/mvc/app/controllers/OPNsense/Core/Api/BackupController.php` (210 lines).
- `/usr/local/opnsense/mvc/app/library/OPNsense/Core/Config.php` (814 lines).
- `/usr/local/www/diag_backup.php` (686 lines).
- `/usr/local/etc/rc` and `/usr/local/etc/rc.bootup`.
- `/usr/local/etc/inc/config.inc` (function `convert_config`).
- `/usr/local/etc/inc/console.inc` (function `is_interface_mismatch`, lines 54-83).
- `/usr/local/opnsense/mvc/script/run_migrations.php`.
- `/usr/local/opnsense/service/conf/actions.d/actions_system.conf`.
- `/usr/local/opnsense/mvc/app/models/OPNsense/Core/ACL/ACL.xml`.
- `find /usr/local/opnsense -path '*Migrations*' -type f` enumerated 80+ per-model migration scripts.
- `configctl 2>&1` action listing.

Upstream verifications, all on `github.com/opnsense/core` tag `25.7` (`bbd245bb098cef5a30faa0221238b823c1e49836`, peeled `17d0a22a053233296c5c80d4f8f2e2f349f2ba7d`):

- `https://raw.githubusercontent.com/opnsense/core/25.7/src/opnsense/mvc/app/library/OPNsense/Core/Config.php` lines 645-680 confirmed `restoreBackup` body identical to live.
- `https://raw.githubusercontent.com/opnsense/core/25.7/src/etc/inc/config.inc` `convert_config` defined at line 137 (build-side line 161; matches the live `25.7.10`-era inc).
- `https://raw.githubusercontent.com/opnsense/core/25.7/src/etc/rc.bootup` ordered call chain confirmed lines 24-60.
- `https://raw.githubusercontent.com/opnsense/core/25.7/src/www/diag_backup.php` lines 240-372 confirmed the GUI restore branches match.
- `https://raw.githubusercontent.com/opnsense/core/25.7/src/opnsense/mvc/app/controllers/OPNsense/Core/Api/BackupController.php` action set confirmed.

Tag references:

- `25.7`     -> `bbd245bb098cef5a30faa0221238b823c1e49836` (peeled `17d0a22a053233296c5c80d4f8f2e2f349f2ba7d`).
- `25.7.10`  -> `bb09f97a80aa586de7c8b4b1a069f146a22fa473` (peeled `c2f076f3041d37d59ba219773e5bb34b0a05c7f7`).

Build-hash note: VM 101 reports build `9af17f0102ca`. `gh api /repos/opnsense/core/commits/9af17f0102ca` returns HTTP 422 ("No commit found"). I treat this as an OPNsense package-build identifier rather than an upstream `core` commit hash. No verifiable source maps it directly. For citation purposes I anchor against tag `25.7` and note that the live files I read match upstream byte-for-byte for the regions inspected.
