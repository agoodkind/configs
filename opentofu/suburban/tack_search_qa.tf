# This resource defines the QA OpenSearch member guest. The guest shape comes
# from the search cluster group_vars, which the render tests also read. The
# disk uses rpool beside the other tack guests and mounts with discard. QA
# runs exactly one member.

locals {
  tack_search = yamldecode(
    file("${path.module}/../../ansible/inventory/group_vars/all/search_cluster.yml")
  )
}

resource "proxmox_virtual_environment_container" "tack_search1_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_search1_suburban.vmid

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    hostname = local.service_mapping.tack_search1_suburban.hostname
    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_search1_suburban.ipv6}/64"
        gateway = local.service_mapping.opnsense_suburban.ipv6_vmnet
      }
    }
    dns {
      servers = [local.service_mapping.dns64_suburban.ipv6]
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
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    mac_address = local.service_mapping.tack_search1_suburban.mac_address
  }

  disk {
    datastore_id  = "local-zfs"
    size          = local.tack_search.tack_search_guest_disk_gib
    mount_options = ["discard"]
  }

  memory {
    dedicated = local.tack_search.tack_search_guest_memory_mib
  }

  cpu {
    cores = local.tack_search.tack_search_guest_cores
  }

  tags = ["lxc", "tack", "tack-search", "qa", "docker"]

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Proxmox does not return injected SSH keys, so a re-import would read
      # the configured keys as an addition that forces replacement.
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}
