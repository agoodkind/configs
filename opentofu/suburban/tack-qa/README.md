# Retire Tack QA state

This root retains the existing encrypted backend and provider identities for state
retirement. It does not provision QA guests or applications.

OpenTofu refreshes stale resources before processing removed blocks. Disable
refresh during retirement planning to avoid contacting deleted guests.

1. Choose a private absolute path for the encrypted saved plan. After approval
   for state reconciliation, run
   `./configsctl tofu suburban/tack-qa plan -refresh=false -out=<private-plan-path>`.
2. Independently inspect the surviving hypervisor and verify the identities and
   ownership of the QA cloud-init snippet, cloud image, and search-caller files.
   The plan does not refresh these assets. Verify that another VM does not use
   the image before approving deletion.
3. Inspect the saved plan. Require the removed blocks to forget deleted guest
   records. Require asset deletions to match the independent hypervisor checks.
4. After separate operator approval, apply that exact saved plan with
   `./configsctl tofu suburban/tack-qa apply <private-plan-path>`.

The search-caller module added the QA address to an existing search proxy file.
Deleting its script and unit does not remove that existing admission entry.
Reconcile the QA-only search proxy configuration through its current owner before
claiming complete host cleanup.

OpenTofu does not accept a keyed instance in a `removed` block. Before planning
the shared Zero Trust root, separately approve and remove only these stale state
addresses after verifying the QA tunnel and guest remain absent:

```bash
./configsctl tofu shared/cloudflare-zero-trust state rm 'cloudflare_zero_trust_tunnel_cloudflared.tack_k3s["qa"]'
./configsctl tofu shared/cloudflare-zero-trust state rm 'cloudflare_zero_trust_tunnel_cloudflared_config.tack_k3s["qa"]'
./configsctl tofu shared/cloudflare-zero-trust state rm 'module.tack_cloudflared["qa"]'
```

Preserve every production instance and its destruction protection. Retiring the
guest base modules also forgets their SMTP relay records. Inspect and retire any
remaining QA SMTP2GO users separately; the deleted guests cannot perform that
cleanup.
