# Retire Tack QA state

This root retains the existing encrypted backend and provider identities for state
retirement. It does not provision QA guests or applications.

1. Review a plan through `./configsctl tofu suburban/tack-qa plan` after approval
   for state reconciliation. The removed blocks forget records on the deleted
   guests without attempting guest access.
2. Inspect deletions for the QA cloud-init snippet, cloud image, and hypervisor
   search-caller files. These assets belong to the surviving hypervisor. Their
   resource addresses do not have `destroy = false`; a later approved apply can
   remove them. Verify that the image does not serve another VM before deletion.
3. Review and apply the retirement plan only with separate operator approval.

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
