# Internal-only SeaweedFS object-store LXC on vault. The production
# twin of the suburban CT 410 store, on the same prod VMNET segment as the tack
# LXC so the prod tack host reaches its S3 endpoint without crossing segments.
# Runs the weed binary under systemd (deploy-seaweedfs.yml); never exposed off
# the segment. Backup destination for the prod tack stores.
resource "proxmox_virtual_environment_container" "seaweedfs" {
  node_name = "vault"
  vm_id     = local.service_mapping.seaweedfs.vmid

  initialization {
    hostname = "seaweedfs.home.goodkind.io"
    ip_config {
      ipv6 {
        address = "3d06:bad:b01::118/64"
        gateway = "3d06:bad:b01::1"
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  features {
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = "vmbr0"
    mac_address = "BC:24:11:00:01:18"
  }

  # The object store's IO is asynchronous backup traffic, so its disk lives on
  # the slow 870 EVO directory storage rather than the P310 thin pool (TACK-495).
  disk {
    datastore_id = "storage"
    size         = 200
  }

  memory {
    dedicated = 4096
  }

  cpu {
    cores = 2
  }

  tags = ["lxc", "seaweedfs", "s3"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
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
      # The provider replaces a container to change its disk's datastore and
      # has no volume move. The disk moves with pct move-volume on the
      # hypervisor instead, and OpenTofu never plans the change itself.
      disk[0].datastore_id,
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

output "seaweedfs_vmid" {
  description = "VMID assigned to the vault SeaweedFS LXC."
  value       = proxmox_virtual_environment_container.seaweedfs.vm_id
}

output "seaweedfs_ipv6" {
  description = "IPv6 address of the vault SeaweedFS LXC."
  value       = "3d06:bad:b01::118"
}
