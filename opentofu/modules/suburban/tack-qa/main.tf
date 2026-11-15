terraform {
  required_providers {
    proxmox = { source = "bpg/proxmox", version = ">= 0.106.0" }
  }
}

variable "service_mapping" {
  type = object({
    tack_qa_suburban      = object({ vmid = number, hostname = string, ipv6 = string, mac_address = string })
    tack_search1_suburban = object({ vmid = number, hostname = string, ipv6 = string, mac_address = string })
    opnsense_suburban     = object({ ipv6_vmnet = string })
  })
}
variable "ssh_keys" { type = string }
variable "host_networks" { type = map(string) }

locals {
  guests = {
    tack_qa_suburban      = { memory_mib = 6144, cores = 2 }
    tack_search1_suburban = { memory_mib = 8192, cores = 2 }
  }
}

resource "proxmox_virtual_environment_container" "service" {
  for_each  = local.guests
  node_name = "hypervisor"
  vm_id     = var.service_mapping[each.key].vmid
  initialization {
    hostname = var.service_mapping[each.key].hostname
    ip_config {
      ipv6 {
        address = "${var.service_mapping[each.key].ipv6}/64"
        gateway = var.service_mapping.opnsense_suburban.ipv6_vmnet
      }
    }
    dns { servers = ["2a07:a8c0::7d:698e", "2a07:a8c1::7d:698e"] }
    user_account { keys = [var.ssh_keys] }
  }
  features { nesting = true }
  network_interface {
    name        = "eth0"
    bridge      = var.host_networks.trunk_suburban
    mac_address = var.service_mapping[each.key].mac_address
  }
  disk {
    datastore_id  = "local-zfs"
    size          = 40
    mount_options = ["discard"]
  }
  memory { dedicated = each.value.memory_mib }
  cpu { cores = each.value.cores }
  tags = ["lxc", "tack", "qa", "docker"]
  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }
  started      = true
  unprivileged = true
  lifecycle {
    ignore_changes = [initialization[0].user_account, operating_system[0].template_file_id]
  }
}

output "tack_qa_suburban_vmid" {
  description = "VMID of the consolidated QA data LXC."
  value       = proxmox_virtual_environment_container.service["tack_qa_suburban"].vm_id
}
output "tack_qa_suburban_ipv6" {
  description = "IPv6 address of the consolidated QA data LXC."
  value       = var.service_mapping.tack_qa_suburban.ipv6
}
