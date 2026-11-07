output "network" { value = {
  isp_sonic_suburban = {
    role = "transport"
    # Masquerade both Sonic provider link networks on the management interface.
    masquerade_interfaces = ["sonic_1", "sonic_2"]
  }
} }
