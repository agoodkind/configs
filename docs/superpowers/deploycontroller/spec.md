# Deploy controllers

Two deploy controller containers run every Configs deploy and every OpenTofu
run. Agent sessions request a run over SSH and do not have a root key for any
guest or hypervisor.

## Defect this replaces

Every lane deploys from the operator's Mac with one root SSH key. Agent
sessions run as the operator's macOS user and use that key through the same
SSH agent. Each guest and hypervisor accepts that key as root. An agent
session can open a root shell on any of them and run any database client.

A restricted key on a guest does not fix this. Ansible runs each task as
`/bin/sh -c` with Python code that it uploads first. A check on the guest
cannot separate an Ansible task from any other shell command.

## Contract

### 1. Controllers

The two controllers are Debian containers with the same configuration.

| Mapping key | Hypervisor |
| --- | --- |
| `deploy_controller_vault` | vault |
| `deploy_controller_suburban` | suburban |

Either controller runs every playbook and every OpenTofu workspace for every
environment. Each controller has its own SSH key pair. Guests and hypervisors
accept each controller key as root.

Each controller stores these files, readable by root only:

- the controller's SSH private key,
- the Ansible vault password,
- the OpenTofu state encryption passphrase and the R2 credentials that the
  OpenTofu backend reads,
- a Configs checkout that the gate updates from `origin/main` for each
  request.

OpenTofu declares each controller completely: the container, its packages,
its files, and its systemd units. No Ansible playbook configures a controller.

### 2. Access

The controller's root `authorized_keys` lists each key that may send requests.
Every entry has this form:

```text
command="/usr/local/bin/configsctl gate --requester <name>",restrict <key>
```

sshd runs the gate for every login with that key. The command that the client
sent is available to the gate only as `SSH_ORIGINAL_COMMAND`. `restrict` turns
off a terminal, port forwarding, and agent forwarding. Configs declares the
requester names and their public keys.

The operator's break-glass key is the only key that opens a shell on a
controller, a guest, or a hypervisor. It is stored in the 1Password SSH agent,
which asks the operator to approve each use.

### 3. Requests

A client sends one JSON object on stdin:

```bash
ssh root@<controller> request < request.json
```

The gate refuses a request when `SSH_ORIGINAL_COMMAND` is not `request`, when
the object has an unknown field, or when a field fails its rule.

| `kind` | Fields | Rule |
| --- | --- | --- |
| `deploy` | `playbook`, `commit`, `limit`, `extra_vars`, `session` | `playbook` is the name of a file in `ansible/playbooks`. `commit` is a 40-character commit reachable from `origin/main`. `extra_vars` is a JSON object. |
| `tofu` | `workspace`, `action`, `commit`, `targets`, `session` | `action` is `plan` or `apply`. `workspace` is the name of a workspace directory. |
| `status` | `run` | `run` is the id of an existing run. |
| `logs` | `run`, `follow` | `run` identifies an existing run. |
| `unlock` | `host`, `run`, `reason` | `run` matches the lock on `host`. `reason` is required. |
| `runs` | none | The gate lists the runs on this controller. |

The gate starts `configsctl` with an argument list. The gate does not execute
request fields as shell code.

The gate supplies `extra_vars` to Ansible as one JSON argument. The JSON
values may contain spaces and quotes.

### 4. Runs

The gate starts each `deploy` and `tofu` request as a transient systemd unit
named `configs-run-<run id>` and returns the run id at once. The run continues
when the client's SSH session ends. `status` returns the unit state and the
exit code. `logs` returns the run output, and `follow` streams it until the run
ends.

The controller writes one record per request to
`/var/lib/configs-runs/<run id>.json` and the system journal. The record
includes the requester, the session field, the request, the Configs commit,
the start and end times, the exit code, and the controller name.

### 5. Locks

Before `configsctl deploy` starts `ansible-playbook`, it resolves the hosts of
`--limit` and takes a lock on each host. A `tofu` run takes a lock on each
hypervisor that its workspace declares.

The lock is the file `/var/lib/configs/deploy.lock` on the host. It contains the
run id, the controller name, and an expiry time. The running controller
renews the expiry every minute. An expiry is 5 minutes after the last renewal.

A run takes the lock when the file is missing or its expiry has passed. A run
that finds a current lock from another run refuses to start. Its error states
the run id and the controller of that lock. An `unlock` request deletes a lock
before its expiry, and the record of that request includes the reason. A run
deletes its locks when it ends.

The lock file is on disk. A host reboot during a run does not delete it.

### 6. Production

The production pause and every later production authorization remain rules
for each agent session. The controllers accept production requests from agent
keys and record each one.

## Boundaries

- An agent key can request any playbook for any environment, production
  included. The controller records the request and does not refuse it.
- Root on a controller is root on every guest and hypervisor. Controllers do
  not run guest application services.
- A controller that loses its network for more than 5 minutes during a run
  stops renewing its locks. Another run may then start on the same hosts. The
  MWAN reboot window in the deploy gate design is shorter than 5 minutes.
- The agent key files are readable by every process of the operator's macOS
  user. The key allows requests only.
- This design does not change the guest services, the playbooks, or the
  OpenTofu workspaces.

## Acceptance criteria

- AC1: Reject an agent-key login if the command is absent or differs from
  `request`, or if the client requests a terminal. Do not open a shell.
- AC2: A request with an unknown field, a playbook outside
  `ansible/playbooks`, or a commit that is not reachable from `origin/main`
  fails before `configsctl` starts.
- AC3: A QA `deploy-tack` request returns a run id, and the deploy finishes
  with `failed=0` after the client disconnects.
- AC4: Execute `tack-ops` on each controller with `tack_ops_command` set to
  `ops search verify`.
- AC5: A second run on a locked host refuses to start, and its error states the
  first run id. After a controller is stopped mid-run, a new run takes the lock
  about 5 minutes later. An `unlock` request frees a lock at once, and the
  controller records it.
- AC6: Run an OpenTofu plan for the `guest` workspace on each controller.
- AC7: Each request has a record with the requester, the session, the commit,
  and the exit code.
- AC8: After the rollout, the shared key is not present on the Mac, and every
  guest and hypervisor refuses it.

## Migration order

1. Add `configsctl gate` and the host locks to configsctl, release it, and pin
   it in Configs.
2. Declare both controllers in OpenTofu, create them, and install their files.
3. Add each controller public key to every guest and hypervisor.
4. Move the Tack deploys to the controllers and prove AC3 through AC7 on QA.
5. Execute each remaining lane's deploys and OpenTofu commands on the
   controllers. Migrate one lane at a time.
6. Move the operator's break-glass key into the 1Password SSH agent. Remove the
   shared key from the Mac, from the GitHub key list that guests read, and
   from every guest and hypervisor.
