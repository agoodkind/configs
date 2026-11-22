# Inspect Vault networking

1. On Vault, list the host interfaces, IP addresses, and default routes.

   ```sh
   ip -br link show
   ip -br address show
   ip -4 route show default
   ip -6 route show default
   ```

2. Find the guest ID and check its status with `pct list` for containers
   or `qm list` for virtual machines.
3. Set `guest_id` to the ID from step 2. Inspect the guest attachment with
   `pct config "$guest_id" --current 1` or `qm config "$guest_id" --current 1`.
4. Run the commands from step 1 inside the running guest. Use
   `pct enter "$guest_id"` for a container or log in to the virtual machine.
