module "host" { source = "../../configuration/hosts/vault" }

# Developer sandbox LXC on vault. Ansible does not manage its inside.
resource "proxmox_virtual_environment_container" "debianct" {
  node_name = module.host.node.name
  vm_id     = local.service_mapping.debianct.vmid

  initialization {
    hostname = local.service_mapping.debianct.hostname
  }

  features {
    nesting = true
    fuse    = true
  }

  disk {
    datastore_id = "local-lvm"
    size         = 82
  }

  memory {
    dedicated = 16384
    swap      = 16384
  }

  cpu {
    architecture = "amd64"
    cores        = 8
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = !contains(var.stopped_guest_ids, local.service_mapping.debianct.vmid)
  start_on_boot = !contains(var.stopped_guest_ids, local.service_mapping.debianct.vmid)
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

variable "service_mapping" { type = object({
  debianct = object({
    vmid     = number
    hostname = string
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

variable "stopped_guest_ids" { type = set(number) }
