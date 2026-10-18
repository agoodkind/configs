terraform {
  required_providers {
    redfish = {
      source  = "registry.terraform.io/dell/redfish"
      version = "1.6.2"
    }
  }
}

variable "nic_functions" {
  description = "Discovered BCM57504 device function IDs mapped to their adapter IDs."
  type        = map(string)
}

variable "maintenance_window" {
  description = "Window in which a separately controlled host reset applies pending NIC settings."
  type = object({
    start_time = string
    duration   = number
  })
}

locals {
  preserved_attribute_names = toset([
    "VirtualizationMode",
    "NumberVFAdvertised",
    "Broadcom_MaxPfMsixVectors",
  ])

  desired_attributes = {
    Broadcom_FlowOffload      = "Enabled"
    Broadcom_MsixVectorsPerVf = "16"
  }

  observed_attribute_names = setunion(
    local.preserved_attribute_names,
    toset(keys(local.desired_attributes)),
  )
}

data "redfish_network" "poweredge" {
  redfish_server {
    redfish_alias = "poweredge"
  }

  nic_filter {
    systems = [{
      system_id = "System.Embedded.1"
      network_adapters = [
        for adapter_id in sort(distinct(values(var.nic_functions))) : {
          network_adapter_id = adapter_id
          network_device_function_ids = toset([
            for function_id, function_adapter_id in var.nic_functions : function_id
            if function_adapter_id == adapter_id
          ])
        }
      ]
    }]
  }

  lifecycle {
    postcondition {
      condition = alltrue([
        for required_attribute in keys(local.desired_attributes) : anytrue([
          for attribute in keys(self.nic_attributes) :
          endswith(attribute, ".${required_attribute}")
        ])
      ])
      error_message = "iDRAC did not return independent NIC attribute readback for the requested settings. Verify the Dell network attributes endpoint before applying."
    }
  }
}

resource "redfish_network_adapter" "bcm57504" {
  for_each = var.nic_functions

  redfish_server {
    redfish_alias = "poweredge"
  }

  system_id                  = "System.Embedded.1"
  network_adapter_id         = each.value
  network_device_function_id = each.key

  # OnReset resets the host before the provider submits its NIC patch.
  apply_time         = "InMaintenanceWindowOnReset"
  maintenance_window = var.maintenance_window

  oem_network_attributes = {
    clear_pending = false
    attributes    = local.desired_attributes
  }

  lifecycle {
    prevent_destroy = true
  }

  depends_on = [data.redfish_network.poweredge]
}

output "observed_nic_attributes" {
  description = "Current iDRAC NIC values for the requested changes and preserved virtualization settings."
  value = {
    for attribute, value in data.redfish_network.poweredge.nic_attributes : attribute => value
    if contains(local.observed_attribute_names, element(reverse(split(".", attribute)), 0))
  }
}
