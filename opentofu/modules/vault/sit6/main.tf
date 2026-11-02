# The LXC uses the host gateway on the private guest network.
resource "proxmox_virtual_environment_container" "sit6" {
  node_name = "vault"
  vm_id     = local.service_mapping.sit6.vmid

  initialization {
    hostname = local.service_mapping.sit6.hostname
    dns {
      servers = [local.service_mapping.dns64.ipv6]
    }
    ip_config {
      ipv6 {
        address = "${local.service_mapping.sit6.ipv6}/64"
        gateway = local.service_mapping.vault_hypervisor.ipv6
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }


  # Feature-flag writes on this privileged container require root@pam.
  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:5E:06:24"
  }

  disk {
    datastore_id = "local-lvm"
    size         = 4
  }

  memory {
    dedicated = 256
    swap      = 0
  }

  cpu {
    architecture = "amd64"
    cores        = 1
  }

  tags = ["lxc", "network", "sit6"]

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = false

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Proxmox does not return injected SSH keys, and the template name is not
      # stored in pct config, so both read as changes that force replacement.
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
      source  = "bpg/proxmox"
      version = ">= 0.106.0"
    }
  }
}
