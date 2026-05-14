# OOB tunnel observability via Cloudflare Logpush

## Status

**Not deployed.** Investigation on 2026-04-27 hit two blockers:

1. The `zero_trust_network_sessions` Logpush dataset (the one that contains
   what we wanted: `DestinationTunnelID`, `ConnectionCloseReason`,
   `IngressColoName`, `EgressColoName`, byte counts, `SessionStartTime`,
   `SessionEndTime`) returns `code 1004: creating a new job (for
   zero_trust_network_sessions dataset) is not allowed: missing required
   permissions`. Adding both `Zero Trust Read` and `Zero Trust Write` perm
   groups to the API token did not change the error. The dataset is likely
   gated to a Zero Trust subscription tier (Enterprise) that this account
   does not have.

2. The R2 destination requires explicit S3-compatible credentials in
   `destination_conf` (format
   `r2://<bucket>?account-id=<acct>&access-key-id=<ak>&secret-access-key=<sk>`).
   Cloudflare's API endpoints for creating R2 S3-compatible tokens
   (`/accounts/<id>/r2/tokens`, `/r2/api_tokens`) returned 404 from the
   account-level token-creator credential. Generating R2 S3 credentials is
   dashboard-only on this account at the moment.

## What we tried

| Step | Result |
|---|---|
| Mint scoped token with `Logs Write` + `Workers R2 Storage Write` + `Cloudflare Tunnel Read` | OK, token works |
| List existing R2 buckets | OK, found `cloudflared-opnsense-pkg` |
| Create R2 bucket `mwan-oob-logpush` | OK |
| Create R2 S3 token via `/accounts/<id>/r2/tokens` | 404 No route matches |
| Create R2 S3 token via `/r2/api_tokens` | 404 No route matches |
| Create Logpush job for `audit_logs` dataset, dest `r2://...?account-id=...` (no S3 keys) | `1002: invalid destination_conf: access-key-id must be provided` |
| Create Logpush job for `zero_trust_network_sessions` (with full S3 keys, hypothetically) | `1004: missing required permissions` (blocked before destination check) |
| Mint token with `Zero Trust Read+Write` added | Same `1004` error on job create |

R2 bucket was deleted at end of investigation. No persistent state left.

## Why this matters

The original goal was to capture per-TCP-session events for the OOB tunnel
that vault cannot see. Specifically: when does a Cloudflare edge unilaterally
close a session, what was the `ConnectionCloseReason`, which `IngressColoName`
was the user routed through. None of that is in any dataset queryable from
vault or via the Cloudflare GraphQL Analytics API at our subscription tier.

Vault-side observability is still solid (see `mwan-ifmgr` and the
`cloudflaredtap` module that mirrors `cloudflared-oob` events into our
unified logging pipeline). What we lose by skipping Logpush: the ability to
post-hoc explain "user perceived OOB drop with zero vault-side correlation"
incidents like the one on 2026-04-27. Vault and Cloudflare GraphQL together
already rule out vault-side and tunnel-control-plane causes; what remains
unobservable is single-edge-PoP unilateral data-plane events.

## What to do later if this becomes worth pursuing

1. Generate R2 S3-compatible credentials via the dashboard
   (`https://dash.cloudflare.com/<acct>/r2/api-tokens`), scope to
   `mwan-oob-logpush` bucket only. Store in 1Password under a new entry.
2. Resolve the `zero_trust_network_sessions` dataset gating. Options:
   * Upgrade Zero Trust plan (paid).
   * File a Cloudflare support ticket asking for the precise perm group
     required (the 1004 error wording is generic).
   * Accept that this dataset is unavailable and pick a less-useful
     dataset that we do have access to.
3. Recreate the R2 bucket and create the Logpush job with full credentials
   in `destination_conf`. Filter on `DestinationTunnelID == "88f11d0d-6148-4670-891d-72c0286ca48d"`.
4. Set up retention policy on the bucket (R2 lifecycle rule, 30 days).
5. Set up a query path: `jq` on downloaded ndjson, or DuckDB pointed at the
   bucket via S3 protocol, or R2 SQL.

## Related

* Plan file: `~/.claude/plans/should-we-test-failover-happy-rocket.md`
  (OOB observability, Task 3).
* MWAN-67: source-based ip6 rule for live MB SLAAC (related but separate
  fix for off-site reachability of vault's public addresses).
* `cloudflared-oob` config on vault: `/etc/cloudflared-oob/config.yml`.
  Tunnel ID: `88f11d0d-6148-4670-891d-72c0286ca48d`.
