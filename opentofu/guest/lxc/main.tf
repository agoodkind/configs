terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = "0.116.0"
    }
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.1"
    }
  }
}

variable "node" {
  type = string
}

variable "guest" {
  type = object({
    vmid         = number
    hostname     = string
    cpu_cores    = number
    memory_mb    = number
    disk_size_gb = number
    options      = map(string)
    devices      = list(string)
    console      = bool
  })
}

variable "template_file_id" {
  type = string
}

variable "datastore_id" {
  type = string
}

variable "restart_on" {
  type    = map(string)
  default = {}
}

variable "forbidden_interfaces" {
  type    = set(string)
  default = []
}

locals {
  hostnic_links = [
    for key, value in var.guest.options : regex("(?:^|,)link=([^,]+)", value)[0]
    if startswith(key, "hostnic")
  ]

  forbidden_hostnic_links = [
    for link in local.hostnic_links : link
    if contains(var.forbidden_interfaces, link)
  ]
}

resource "proxmox_virtual_environment_container" "this" {
  node_name = var.node
  vm_id     = var.guest.vmid

  initialization {
    hostname = var.guest.hostname
  }

  features {
    nesting = true
  }

  disk {
    datastore_id = var.datastore_id
    size         = var.guest.disk_size_gb
  }

  memory {
    dedicated = var.guest.memory_mb
    swap      = 0
  }

  cpu {
    architecture = "amd64"
    cores        = var.guest.cpu_cores
    limit        = 0
  }

  dynamic "console" {
    for_each = var.guest.console ? [true] : []

    content {
      enabled   = true
      tty_count = 2
      type      = "tty"
    }
  }

  dynamic "device_passthrough" {
    for_each = var.guest.devices

    content {
      path = device_passthrough.value
      mode = "0660"
    }
  }

  operating_system {
    template_file_id = var.template_file_id
    type             = "debian"
  }

  started       = false
  start_on_boot = false
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    ignore_changes  = [operating_system[0].template_file_id, started]

    postcondition {
      condition     = length(self.network_interface) == 0
      error_message = "Container ${var.guest.hostname} must use hostnicN options without network_interface blocks."
    }
  }
}

resource "pveguest_container_options" "this" {
  node    = var.node
  vmid    = proxmox_virtual_environment_container.this.vm_id
  options = var.guest.options

  lifecycle {
    precondition {
      condition     = length(local.forbidden_hostnic_links) == 0
      error_message = "Container ${var.guest.hostname} cannot use these host interfaces: ${join(", ", local.forbidden_hostnic_links)}"
    }
  }
}

resource "pveguest_container_power" "this" {
  node    = var.node
  vmid    = proxmox_virtual_environment_container.this.vm_id
  running = true

  restart_on = merge(
    var.restart_on,
    { options = jsonencode(pveguest_container_options.this.options) },
  )
}

output "vmid" {
  value = pveguest_container_power.this.vmid
}

output "options" {
  value = pveguest_container_options.this.options
}
