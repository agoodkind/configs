terraform {
  required_providers {
    redfish = {
      source  = "registry.terraform.io/dell/redfish"
      version = "1.6.2"
    }
    restful = {
      source  = "registry.terraform.io/magodo/restful"
      version = "0.25.2"
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
}

data "restful_resource" "nic_attributes" {
  for_each = var.nic_functions

  id     = "/redfish/v1/Chassis/System.Embedded.1/NetworkAdapters/${each.value}/NetworkDeviceFunctions/${each.key}/Oem/Dell/DellNetworkAttributes/${each.key}"
  method = "GET"
  output_attrs = toset([
    for attribute in local.observed_attribute_names : "Attributes.${attribute}"
  ])

  lifecycle {
    postcondition {
      condition = alltrue([
        for attribute in local.observed_attribute_names :
        can(self.output.Attributes[attribute])
      ])
      error_message = "iDRAC did not return the required active NIC attributes. Verify the DellNetworkAttributes endpoint before applying."
    }
  }
}

check "active_nic_attributes" {
  assert {
    condition = alltrue(flatten([
      for function_id, readback in data.restful_resource.nic_attributes : [
        for attribute, value in local.desired_attributes :
        tostring(readback.output.Attributes[attribute]) == value
      ]
    ]))
    error_message = "Active BCM57504 attributes differ from the declared values. Read pending iDRAC jobs and verify after the controlled reset."
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

  depends_on = [
    data.redfish_network.poweredge,
    data.restful_resource.nic_attributes,
  ]
}

output "observed_nic_attributes" {
  description = "Current iDRAC NIC values for the requested changes and preserved virtualization settings."
  value = {
    for function_id, readback in data.restful_resource.nic_attributes :
    function_id => readback.output.Attributes
  }
}
