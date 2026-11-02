# Infrastructure overview

Vault, in San Francisco, runs production LXCs. Its host provides the internal guest network and outbound translation over an ordinary upstream connection. Suburban, in New Jersey, runs the routing testbed.

Cloudflare tunnels provide private guest access and publish web services. Cloudflare hosts DNS for the domain.
