module "host" { source = "../../configuration/hosts/vault" }

resource "proxmox_virtual_environment_container" "proxy" {
  node_name = module.host.node.name
  vm_id     = local.service_mapping.proxy.vmid

  initialization {
    hostname = split(".", local.service_mapping.proxy.hostname)[0]
    dns {
      domain = regex("^[^.]+\\.(.+)$", local.service_mapping.proxy.hostname)[0]
    }
  }

  features {
    nesting = true
  }

  disk {
    datastore_id = "local-lvm"
    size         = 8
  }

  memory {
    dedicated = 2048
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 2
  }

  tags = ["legacyv4", "lxc"]

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "ubuntu"
  }

  started       = !contains(var.stopped_guest_ids, local.service_mapping.proxy.vmid)
  start_on_boot = !contains(var.stopped_guest_ids, local.service_mapping.proxy.vmid)
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      operating_system[0].template_file_id,
    ]
  }
}

variable "service_mapping" { type = object({
  proxy = object({
    vmid     = number
    hostname = string
    ipv4     = string
    ipv6     = string
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
