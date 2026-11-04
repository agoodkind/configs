# Infrastructure overview

Vault, in San Francisco, runs production LXCs. Its host provides the internal guest network and outbound translation over an ordinary upstream connection. Suburban, in New Jersey, runs the routing testbed.

Cloudflare tunnels provide private guest access and publish web services. Cloudflare hosts DNS for the domain.

OpenTofu owns nonsecret host and feature configuration. Each feature imports the shared identities and defaults that it uses. Guest preparation and controller lock selection read the same native guest definitions. Provider-free roots evaluate the selected feature for controller metadata and host artifacts.

These details are snapshots. Verify the running host before changing production.
