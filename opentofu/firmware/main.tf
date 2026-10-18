module "poweredge" {
  source = "./poweredge"

  nic_functions      = var.poweredge_nic_functions
  maintenance_window = var.poweredge_maintenance_window
}

output "observed_nic_attributes" {
  description = "Independent iDRAC NIC attribute readback. The provider resource does not refresh configured OEM attribute values."
  value       = module.poweredge.observed_nic_attributes
}
