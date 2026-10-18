module "poweredge" {
  source = "./poweredge"

  nic_functions = {
    "NIC.Embedded.1-1-1" = "NIC.Embedded.1"
    "NIC.Embedded.2-1-1" = "NIC.Embedded.1"
    "NIC.Embedded.3-1-1" = "NIC.Embedded.1"
    "NIC.Embedded.4-1-1" = "NIC.Embedded.1"
  }
  maintenance_window = var.poweredge_maintenance_window
}

output "observed_nic_attributes" {
  description = "Independent iDRAC NIC attribute readback. The provider resource does not refresh configured OEM attribute values."
  value       = module.poweredge.observed_nic_attributes
}
