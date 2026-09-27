# This resource defines the production OpenSearch member guest. The guest
# shape comes from the search cluster group_vars, which the render tests also
# read. The disk uses the P310 thin pool beside the other tack guests and
# mounts with discard (TACK-498). Production starts with this one member.
# Each later member is a separate guest.

locals {
  tack_search = yamldecode(
    file("${path.module}/../../ansible/inventory/group_vars/all/search_cluster.yml")
  )
}

resource "proxmox_virtual_environment_container" "tack_search1" {
  node_name = "vault"
  vm_id     = local.service_mapping.tack_search1.vmid

  initialization {
    hostname = local.service_mapping.tack_search1.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_search1.ipv6}/64"
        gateway = local.service_mapping.opnsense.ipv6
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
    mac_address = local.service_mapping.tack_search1.mac_address
  }

  disk {
    datastore_id  = "local-lvm"
    size          = local.tack_search.tack_search_guest_disk_gib
    mount_options = ["discard"]
  }

  memory {
    dedicated = local.tack_search.tack_search_guest_memory_mib
  }

  cpu {
    cores = local.tack_search.tack_search_guest_cores
  }

  tags = ["lxc", "tack", "tack-search", "docker"]

  operating_system {
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  lifecycle {
    prevent_destroy = true
    # Proxmox returns neither the injected SSH keys nor the template name.
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}
