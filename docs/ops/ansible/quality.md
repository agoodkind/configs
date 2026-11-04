# Validate configuration

Run these checks from the repository root.

1. Check native formatting and provider-free ownership inputs:

   ```bash
   ./configsctl lint
   ```

2. Run the artifact helper checks and existing release-verification fixtures:

   ```bash
   make -C hostartifact check
   ```

3. Initialize the provider-free artifact policy module:

   ```bash
   tofu -chdir=opentofu/modules/overlay/host-artifacts init -backend=false -input=false
   ```

4. Run its native policy tests:

   ```bash
   tofu -chdir=opentofu/modules/overlay/host-artifacts test
   ```

The artifact release checks require GNU checksum tools and authenticated GitHub access. These checks evaluate configuration and rendered output without changing a host or infrastructure state.

## KEA DHCP reservations

- DHCPv4: `hw-address` and `client-id` are mutually exclusive.
- DHCPv6: `hw-address` and `duid` are mutually exclusive.

Use one identifier per reservation. Prefer `hw-address` when the MAC is pinned.
