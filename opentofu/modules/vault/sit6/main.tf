# 6in4 tunnel endpoint LXC on vault for Berylax. Privileged so the guest can
# create its sit tunnel device; vault loads the sit module at boot. The only
# NIC is on mwanbr, with the MWAN VM's transit addresses as default gateways.
resource "proxmox_virtual_environment_container" "sit6" {
  node_name = "vault"
  vm_id     = local.service_mapping.sit6.vmid

  initialization {
    hostname = local.service_mapping.sit6.hostname
    ip_config {
      ipv4 {
        address = "${local.service_mapping.sit6.ipv4}/29"
        gateway = "10.250.250.3"
      }
      ipv6 {
        address = "${local.service_mapping.sit6.ipv6}/64"
        gateway = "3d06:bad:b01:fe::3"
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  # No features block. The tunnel, FRR, and nftables need no container
  # features, and Proxmox refuses feature-flag writes from any actor but
  # root@pam on a privileged container.

  network_interface {
    name        = "eth0"
    bridge      = "mwanbr"
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
