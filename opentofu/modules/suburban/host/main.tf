module "config" {
  source = "../../configuration/hosts/suburban"
}

resource "proxmox_network_linux_bridge" "vm_management_suburban" {
  node_name = "hypervisor"
  name      = module.config.bridges.vm_management_suburban

  autostart = true
  address   = "${local.service_mapping.vmbr1_suburban.ipv4}/24"
  address6  = "fe80::1/64"

  lifecycle {
    prevent_destroy = true
  }
}

variable "service_mapping" { type = object({
  vmbr1_suburban = object({
    ipv4 = string
  })
}) }


locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"

    }
  }
}
