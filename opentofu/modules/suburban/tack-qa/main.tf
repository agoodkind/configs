terraform {
  required_providers {
    proxmox = { source = "bpg/proxmox", version = ">= 0.106.0" }
  }
}

variable "service_mapping" {
  type = object({
    tack_qa_suburban      = object({ vmid = number, hostname = string, ipv4_cidr = string, ipv6 = string, mac_address = string })
    tack_search1_suburban = object({ vmid = number, hostname = string, ipv4_cidr = string, ipv6 = string, mac_address = string })
  })
}
variable "host" {
  type = object({ node_name = string, dns_servers = list(string), datastore_id = string })
}
variable "capacity" {
  type = map(object({ memory_mib = number, cores = number, disk_gib = number }))
}
variable "template_file_id" { type = string }
variable "ssh_keys" { type = string }
variable "vmnet" {
  type = object({ bridge = string, ipv4_gateway = string, ipv6_gateway = string })
}

resource "proxmox_virtual_environment_container" "service" {
  for_each  = var.capacity
  node_name = var.host.node_name
  vm_id     = var.service_mapping[each.key].vmid
  initialization {
    hostname = var.service_mapping[each.key].hostname
    ip_config {
      dynamic "ipv4" {
        for_each = var.service_mapping[each.key].ipv4_cidr == null ? [] : [var.service_mapping[each.key].ipv4_cidr]
        content {
          address = ipv4.value
          gateway = var.vmnet.ipv4_gateway
        }
      }
      ipv6 {
        address = "${var.service_mapping[each.key].ipv6}/64"
        gateway = var.vmnet.ipv6_gateway
      }
    }
    dns { servers = var.host.dns_servers }
    user_account { keys = [var.ssh_keys] }
  }
  features { nesting = true }
  network_interface {
    name        = "eth0"
    bridge      = var.vmnet.bridge
    mac_address = var.service_mapping[each.key].mac_address
  }
  disk {
    datastore_id  = var.host.datastore_id
    size          = each.value.disk_gib
    mount_options = ["discard"]
  }
  memory { dedicated = each.value.memory_mib }
  cpu { cores = each.value.cores }
  tags = ["lxc", "tack", "qa", "docker"]
  operating_system {
    template_file_id = var.template_file_id
    type             = "debian"
  }
  started      = true
  unprivileged = true
  lifecycle {
    ignore_changes = [initialization[0].user_account, operating_system[0].template_file_id]
  }
}

output "tack_qa_suburban_vmid" {
  description = "This output returns the VMID of the consolidated QA data LXC."
  value       = proxmox_virtual_environment_container.service["tack_qa_suburban"].vm_id
}
output "tack_qa_suburban_ipv6" {
  description = "This output returns the IPv6 address of the consolidated QA data LXC."
  value       = var.service_mapping.tack_qa_suburban.ipv6
}
