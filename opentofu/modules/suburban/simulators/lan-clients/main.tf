resource "proxmox_virtual_environment_container" "mwan_test_client_suburban" {
  for_each  = local.mwan_test_clients
  node_name = "hypervisor"
  vm_id     = each.value.vmid


  initialization {
    hostname = each.value.hostname
    ip_config {
      ipv4 {
        address = "${each.value.ipv4}/24"
        gateway = local.service_mapping.opnsense_suburban.ipv4_privileged
      }
      ipv6 {
        address = "${each.value.ipv6}/64"
        gateway = local.service_mapping.opnsense_suburban.ipv6_privileged
      }
    }
    dns {
      servers = [local.service_mapping.dns64_suburban.ipv6]
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  network_interface {
    name        = "eth0"
    bridge      = var.host_networks.trunk_suburban
    vlan_id     = 100
    mac_address = each.value.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    size         = 4
  }

  memory {
    dedicated = 256
  }

  cpu {
    cores = 1
  }

  tags = ["lxc", "mwan", "testbed"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

locals {
  mwan_test_clients = {
    a = local.service_mapping.mwan_test_client_a_suburban
    b = local.service_mapping.mwan_test_client_b_suburban
  }
}

variable "service_mapping" { type = any }

variable "ssh_keys" { type = string }

variable "host_networks" { type = map(string) }

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"

    }
  }
}

output "mwan_test_clients" {
  description = "The suburban VLAN 100 test clients use these VMIDs and addresses."
  value = {
    for name, client in local.mwan_test_clients : name => {
      vmid = proxmox_virtual_environment_container.mwan_test_client_suburban[name].vm_id
      ipv4 = client.ipv4
      ipv6 = client.ipv6
    }
  }
}
