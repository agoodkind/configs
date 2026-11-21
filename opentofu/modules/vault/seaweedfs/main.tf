resource "proxmox_virtual_environment_container" "seaweedfs" {
  node_name = module.host.node.name
  vm_id     = local.service_mapping.seaweedfs.vmid

  initialization {
    hostname = split(".", local.service_mapping.seaweedfs.hostname)[0]
    dns {
      domain = regex("^[^.]+\\.(.+)$", local.service_mapping.seaweedfs.hostname)[0]
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  features {
    nesting = true
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

  started       = !contains(var.stopped_guest_ids, local.service_mapping.seaweedfs.vmid)
  start_on_boot = !contains(var.stopped_guest_ids, local.service_mapping.seaweedfs.vmid)
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

module "host" { source = "../../configuration/hosts/vault" }

variable "service_mapping" { type = object({
  seaweedfs = object({
    vmid     = number
    hostname = string
  })
}) }

variable "ssh_keys" { type = string }

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"
    }
  }
}

variable "stopped_guest_ids" { type = set(number) }

output "seaweedfs_vmid" {
  description = "VMID assigned to the vault SeaweedFS LXC."
  value       = proxmox_virtual_environment_container.seaweedfs.vm_id
}

output "seaweedfs_ipv6" {
  description = "IPv6 address of the vault SeaweedFS LXC."
  value       = "3d06:bad:b01::118"
}
