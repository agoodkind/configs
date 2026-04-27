# Tack reset investigation, 2026-04-25

## Update at 2026-04-25 ~21:00 UTC: data is back

A second Tack rebuild later in the day created a fresh org `019dc5ad-0408-7e43-9c4d-d3e6736ac058` with the prior tickets restored: 375 issues across 7 projects (TACK 160, CLYDE 93, MWAN 46, LAB 33, APP 17, OSS 14, WEBSITE 12). The MWAN reconciliation pass then marked 17 prior-shipped issues as `done` and added 5 new issues for today's findings. The earlier conclusion that "data is unrecoverable from this FDB instance" turned out to be wrong: the data was restored from an external source (likely Clyde session history or a backup) into the new org, then surfaced once the MCP authenticated against the new active org.

The bootstrap-creates-new-org behavior described below is still real and architecturally fragile. But it is now clear that there is a separate "ticket re-import" pathway that can rehydrate the new org from an external source. Tickets with sequence numbers like MWAN-30..33 (today's bootstrap timing) have descriptions identical to what I created on 2026-04-12, suggesting they were rehydrated verbatim from session memory or a backup. Tickets MWAN-1..MWAN-12 read as raw user-quote scrapes, suggesting a separate ingestion source.

The architectural problems flagged in section 3 below remain valid: silent-empty MCP responses, ghost orgs in `org_members`, no two-store invariant.

## Original TL;DR

The Tack server was rebuilt on the tack host (CT 117) at `2026-04-24 22:31 PDT` / `2026-04-25 05:31 UTC` via a `docker compose up --build`. The new app build was hash `95b398e-dirty` from build_time `2026-04-25T05:31:43Z`. Two restarts followed within 20 seconds. After the second restart the app **bootstrapped a brand new org `goodkind-io` (UUID `019dc320-ce6f-770f-9655-d2b56b7ddc20`)** and created seven default projects (APP, CLYDE, LAB, MWAN, OSS, TACK, WEBSITE) plus a single workspace `main` under it. None of the prior data was carried forward.

The existing FDB volume (`fdb-data`) was NOT recreated. The FDB and Yugabyte containers have been continuously running since `2026-04-13 07:22 UTC`. The reset is therefore an **application-layer bootstrap**, not an infrastructure wipe.

## Concrete evidence

### Container ages

| Container | Created | Last started | Restarts |
|---|---|---|---|
| `tack-app-1` | 2026-04-25 05:33:05 UTC | 2026-04-25 05:33:26 UTC | 0 |
| `tack-fdb-1` | 2026-04-09 03:49:03 UTC | 2026-04-13 07:22:35 UTC | 0 |
| `tack-yugabyte-1` | 2026-04-08 16:22:54 UTC | 2026-04-13 07:22:35 UTC | 0 |

The `tack-app-1` container is brand new (3 hours old at investigation time) but FDB and Yugabyte have been up for 12 days continuously, no restart count. So the data stores were not reinitialized; only the app process was.

### Build trace (from `journalctl -u docker`)

```
Apr 24 22:31:49 ... [runtime 2/3] RUN apt-get install ca-certificates curl ... foundationdb-clients_7.4.6-1
Apr 24 22:32:02 ... [builder 2/7] RUN curl ... foundationdb-clients_7.4.6-1
Apr 24 22:32:07 ... [builder 5/7] RUN go mod download
Apr 24 22:32:14 ... [builder 7/7] RUN CGO_ENABLED=1 go build -tags fdb ...
Apr 24 22:32:53 ... exporting to image
Apr 24 22:33:05 ... stopping container d146df... (previous tack-app-1)
Apr 24 22:33:07 ... new container start
Apr 24 22:33:25 ... stopping container b855... (just-started tack-app-1)
Apr 24 22:33:26 ... new container start (the live one)
```

This is a manual `docker compose up --build` cycle. There is no scheduler or cron entry that would have triggered it; whoever was logged into the tack host ran it at 22:31 PDT.

### App startup creates the org

```
05:33:07 UTC  starting server  version=95b398e-dirty
05:33:13      first MCP request from 172.18.0.1
...
05:33:25      shutting down       (first instance receives SIGTERM)
05:33:26      starting server     (second instance, the current live one)
05:33:32      first MCP request to second instance
05:33:37      node.Create  node_id=019dc321-11e9-7c3f-ae32-71be048c12d7  node_type=project  (this is project "TACK")
```

The first project creation timestamp is `05:33:37 UTC`. Within roughly 30 seconds of app startup, the new org and seven projects were created. They were created **via the MCP** (the requests came from `172.18.0.1` which is the docker bridge gateway, i.e. another container on the host calling the MCP, possibly an init script or the MCP client running in `tack-app-1` itself).

### Yugabyte org_members table (the surviving record)

```
user_id                              | org_id                              | role | created_at
4fdb794c-da8f-4931-bf17-2bcf651be731 | c5c84639-8e60-4606-b81a-fb0fd6b0b4cb | 20   | 2026-04-12 18:50:18 UTC
4fdb794c-da8f-4931-bf17-2bcf651be731 | f8239d3a-3bf1-43d5-b74a-36a71e4101a1 | 20   | 2026-04-12 22:19:08 UTC
4fdb794c-da8f-4931-bf17-2bcf651be731 | 80004488-c9f1-4ba6-80ab-d2205f3c8108 | 20   | 2026-04-13 01:31:09 UTC
4fdb794c-da8f-4931-bf17-2bcf651be731 | 019dae93-92a8-7713-b295-dd840a504a8a | 20   | 2026-04-21 05:46:40 UTC
4fdb794c-da8f-4931-bf17-2bcf651be731 | 019dc320-ce6f-770f-9655-d2b56b7ddc20 | 20   | 2026-04-25 05:33:20 UTC
```

Five org membership rows for one user. The first three line up with the burst of Tack work on Apr 12-13 (the cutover2 sessions). The Apr 21 row is unaccounted for. The Apr 25 row is the bootstrap that just happened.

### FDB keyspace

Every key is scoped by org_id. The only org_id ever appearing in FDB is `019dc320-ce6f-770f-9655-d2b56b7ddc20` (today's bootstrap). The other four org_ids referenced in `org_members` have **zero keys** in FDB. That means:

- Either the prior orgs' data was wiped before today's rebuild, or
- The prior orgs were created in a Yugabyte-only era (before FDB was the node store) and the FDB has only ever held today's data.

The FDB data dir is `134M` and contains a single `node_by_property` / `node_instance` / `node_view` triple per object plus indexes. The total key count is on the order of ~100, all from today's bootstrap. No "issue" type instances exist anywhere; the projects were created empty.

There is **no** `migrations` or `bootstrap` log entry that says "wiping FDB" or "migrating from yugabyte to FDB". The data either silently disappeared or was never there.

## The three architectural problems the user flagged

### 1. "Why did the reset happen?"

The reset is not a single event. There are two layers:

1. **App rebuild** (clear cause): a manual `docker compose up --build` triggered at `2026-04-24 22:31 PDT`. New binary, same data volumes.

2. **Org/project re-bootstrap** (the surprising part): on first start of the new build, the app called the MCP to create a new org `goodkind-io` and seven default projects. It did not look for existing orgs in Yugabyte that the authenticated user already belonged to. It just created a fresh one.

The bootstrap behavior is the architectural bug. The app should either:
- (a) detect existing org_members rows in Yugabyte for the bootstrap user and bind to one of those orgs, or
- (b) refuse to bootstrap if the FDB has org rows already, or
- (c) require an explicit `--bootstrap` flag and fail fast otherwise.

Without one of those, every clean rebuild will spawn another orphan org. The Apr 21 org is likely from a previous rebuild that did the same thing.

### 2. "Why did so many orgs get created?"

Five org_members rows correspond to five distinct rebuild events:

- 2026-04-12 18:50 (cutover2 work begin)
- 2026-04-12 22:19 (later in the cutover2 evening)
- 2026-04-13 01:31 (next cutover2 push)
- 2026-04-21 05:46 (unknown rebuild; possibly during a quieter session)
- 2026-04-25 05:33 (today's rebuild)

Each rebuild called the bootstrap path and created a new org. The user was auto-added to each. The old orgs were never cleaned up; the rows still exist in `org_members`. Only the most recent org has FDB content.

This explains why the MCP returned the *wrong* workspace earlier: `tack_list_workspaces` traversed the user's org memberships and returned one of the orphan orgs (the Apr 21 one, `019dae93...`). That org had no FDB content, hence `tack_list_projects` returned `[]`.

The **schema-on-bootstrap-only** model in this codebase is fragile: any rebuild produces a ghost org. Yugabyte still trusts those ghost orgs because they're real rows; FDB has no record of them. The two stores diverge silently.

### 3. "Why did this happen mid-conversation during MCP tool use?"

It did not. The rebuild happened at `2026-04-24 22:31 PDT` (`2026-04-25 05:31 UTC`). The MCP queries we ran today were hours later (about `2026-04-25 14:50 UTC` and onward). The rebuild and the MCP failures are sequential, not concurrent.

What made it look mid-conversational is that the **failure mode is silent**: the MCP returned `200 OK` with `[]` instead of an error. So the symptom only surfaced when we tried to use Tack today and saw missing tickets. Nothing in the live MCP exchanges triggered a state change; the state was already wrong before this conversation began.

The deeper issue is the silent-empty response. `tack_list_projects workspace_slug=main` should have:
- (a) returned an error if `main` resolved to multiple workspaces across orgs (ambiguous), or
- (b) returned an error if the resolved workspace had no FDB content despite Yugabyte saying the user is a member (data inconsistency), or
- (c) been routed to the active/default org explicitly, with the other orgs hidden.

Returning `[]` with `200 OK` for an org that has no FDB content is the worst of all worlds: the consumer can't tell whether the project list is genuinely empty, the auth is wrong, or the data is missing.

## Recommendations

| Issue | Fix |
|---|---|
| Bootstrap creates new org on every clean install | Bootstrap should be a one-shot CLI subcommand (`tack bootstrap`) gated behind an explicit flag, not a side effect of app start. |
| `tack_list_workspaces` exposes ghost orgs | The MCP should return only orgs that exist in BOTH yugabyte (`org_members`) AND FDB (`node_by_property` for org). Reconciliation on read. |
| Ghost orgs in `org_members` never cleaned up | After bootstrap reform, write a one-time migration to delete `org_members` rows for org_ids missing from FDB. |
| Silent-empty results for missing workspace | `tack_list_projects` should return an error when the workspace's org_id has no `node_instance` keys in FDB. |
| Data loss across rebuilds | If the bootstrap is intended to be reusable, then on rebuild the app should bind to the most-recent existing org for the user; otherwise it should refuse to start. |
| Two-store consistency invariant unenforced | Any write that touches Yugabyte (`org_members`) must also write the corresponding FDB rows in the same transaction, or both should be rolled back. Currently they appear to be written independently. |

## What we lost

- All MWAN tickets created during the 2026-04-12/13 cutover2 sessions (~29 by my count, possibly more if the user mentioned 400+).
- Tickets in any other project that was active in prior orgs (APP, CLYDE, LAB, OSS, TACK, WEBSITE) under those org_ids.
- All comments, labels, states, and relationships scoped to those orgs.

The data is unrecoverable from this FDB instance. If a backup exists for the prior FDB volume, it would need to be restored separately. Otherwise the lost tickets need to be recreated from session memory, which is incomplete.
