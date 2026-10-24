resource "proxmox_virtual_environment_container" "isp_webpass_suburban" {
  provider  = proxmox.root
  node_name = "hypervisor"
  vm_id     = local.service_mapping.isp_webpass_suburban.vmid


  initialization {
    hostname = local.service_mapping.isp_webpass_suburban.hostname
    user_account {
      keys = [var.ssh_keys]
    }
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_webpass_suburban.ipv4}/${local.service_mapping.isp_webpass_suburban.ipv4_prefix}"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.isp_webpass_suburban.ipv4_uplink}/24"
        gateway = local.service_mapping.vmbr1_suburban.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.isp_webpass_suburban.ipv6_uplink}/64"
        gateway = local.service_mapping.vmbr1_suburban.ipv6
      }
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = var.host_networks.isp_webpass_suburban
    mac_address = "BC:24:11:7F:DE:4E"
  }

  network_interface {
    name        = "eth1"
    bridge      = var.host_networks.vm_management_suburban
    mac_address = "BC:24:11:FC:17:A7"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 2
  }

  memory {
    dedicated = 512
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 1
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = []

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = false

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

variable "service_mapping" { type = any }

variable "ssh_keys" { type = string }

variable "host_networks" { type = map(string) }

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source                = "bpg/proxmox"
      version               = ">= 0.106.0"
      configuration_aliases = [proxmox.root]
    }
  }
}
