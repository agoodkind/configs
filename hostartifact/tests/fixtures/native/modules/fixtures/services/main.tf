output "service_mapping" { value = {
  "hypervisor" = {
    "vmid" = 100
  }
  "mwan" = {
    "vmid" = 113
    "address" = "3d06:bad:b01::113"
  }
  "testbed_mwan" = {
    "vmid" = 213
    "address" = "3d06:bad:b01:210::213"
  }
} }
