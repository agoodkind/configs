# Infrastructure overview

Vault runs the production service containers and the Tack Kubernetes VM in San Francisco. Its OpenTofu configuration assigns the management port to the hypervisor and a separate SFP port to guest virtual functions. Suburban runs the routing testbed in New Jersey.

Cloudflare tunnels provide private guest access and publish web services. Cloudflare hosts DNS for the domain.

OpenTofu owns nonsecret host and feature configuration. Each feature imports the shared identities and defaults that it uses. Guest preparation and controller lock selection read the same native guest definitions. Provider-free roots evaluate the selected feature for controller metadata and host artifacts.

Use the [Vault migration procedure](ops/infra/vault.md) to move existing guests and verify the live network.
