# Permission reference

## Privileges

Every privilege below belongs to the `root` privilege group. Custom roles can list them. Among the built-in roles, only `Administrator` includes them.

| Privilege | ACL path | Operation |
| --- | --- | --- |
| `VM.Config.Nesting` | `/vms/<vmid>` | Change the `nesting` flag in the `features` option of a container |
| `VM.Config.Keyctl` | `/vms/<vmid>` | Change the `keyctl` flag in the `features` option of a container |
| `VM.Config.Vsock` | `/vms/<vmid>` | Set, delete, or revert the `vsock` option of a VM |
| `Sys.ACME.Account.Audit` | `/acme/accounts/<name>` | Read one ACME account and see it in the account list |
| `Sys.ACME.Account.Create` | `/acme/accounts/<name>` | Register an ACME account with that name |
| `Sys.ACME.Account.Modify` | `/acme/accounts/<name>` | Update or refresh one ACME account |
| `Sys.ACME.Account.Remove` | `/acme/accounts/<name>` | Deactivate and delete one ACME account |

## ACL paths

| Path | Scope |
| --- | --- |
| `/acme` | Every ACME account, with propagation |
| `/acme/accounts` | Every ACME account, with propagation |
| `/acme/accounts/<name>` | One ACME account |

An account name starts with a letter and continues with one or more letters, digits, `_`, or `-`. The access control module rejects every other path under `/acme`.

## Container feature flags

`PUT /nodes/{node}/lxc/{vmid}/config` accepts a caller with any `VM.Config` privilege on `/vms/{vmid}`, including `VM.Config.Nesting` and `VM.Config.Keyctl`. Each option then has its own check.

The `features` check compares the stored flags with the requested flags. A missing boolean flag equals `0`. `delete=features` counts as a change of every stored flag.

| Changed flag | Privileged container | Unprivileged container |
| --- | --- | --- |
| `nesting` | `VM.Config.Nesting` | `VM.Config.Nesting` or `VM.Allocate` |
| `keyctl` | `VM.Config.Keyctl` | `VM.Config.Keyctl` |
| Any other flag | `root@pam` only | `root@pam` only |

A request that changes several flags needs the privilege for each changed flag. A flag that keeps its value needs no privilege.

Container creation and restore use the same `features` check with no stored flags. A privileged container still requires `Sys.Modify` on `/` at creation and restore.

## VM vsock option

`vsock` is a boolean VM option with default `0`. With `vsock: 1`, the QEMU command has `-device vhost-vsock-pci,id=vsock0,guest-cid=<vmid>,bus=pci.1,addr=0x1f`.

| Operation | Requirement |
| --- | --- |
| Set, delete, or revert `vsock` through `POST` or `PUT /nodes/{node}/qemu/{vmid}/config` | `VM.Config.Vsock` on `/vms/{vmid}` |
| Create or restore a VM with `vsock` | `VM.Config.Vsock` on `/vms/{vmid}` in addition to the stock creation privileges |
| Clone a VM with `vsock: 1` | Stock clone privileges. The clone copies the option. |
| Set `args` | `root@pam` only, unchanged |

`VM.Config.Options` and `VM.Config.HWType` do not authorize a `vsock` change. A change on a running VM stays pending until the next cold start.

## ACME account methods

| Method | Path | Requirement |
| --- | --- | --- |
| `GET` | `/cluster/acme/account` | Any authenticated user. The result lists only accounts where the caller has `Sys.ACME.Account.Audit`. |
| `POST` | `/cluster/acme/account` | `Sys.ACME.Account.Create` on `/acme/accounts/<name>`. A request without `name` uses `default`. |
| `GET` | `/cluster/acme/account/{name}` | `Sys.ACME.Account.Audit` on `/acme/accounts/{name}` |
| `PUT` | `/cluster/acme/account/{name}` | `Sys.ACME.Account.Modify` on `/acme/accounts/{name}` |
| `DELETE` | `/cluster/acme/account/{name}` | `Sys.ACME.Account.Remove` on `/acme/accounts/{name}` |

`root@pam` passes every check. The overlay changes no other ACME method. DNS plugin methods require `Sys.Modify` on `/`. Certificate order, renewal, and revocation require `Sys.Modify` on `/nodes/{node}`.

## Tokens

A token with `privsep=1` has a privilege on a path only when both the token and the owning user have it there. A token ACL entry without a matching user ACL entry grants no access.
