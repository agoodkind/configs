import {
  to = proxmox_virtual_environment_container.unifi
  id = "vault/102"
}

resource "proxmox_virtual_environment_container" "unifi" {
  node_name     = "vault"
  vm_id         = module.vault_network.guest_networks.unifi.vmid
  started       = !contains(var.stopped_guest_ids, module.vault_network.guest_networks.unifi.vmid)
  start_on_boot = !contains(var.stopped_guest_ids, module.vault_network.guest_networks.unifi.vmid)
  unprivileged  = true
  initialization { hostname = "unifi-docker" }
  cpu {
    architecture = "amd64"
    cores        = 2
  }
  memory {
    dedicated = 4096
    swap      = 512
  }
  features {
    nesting = true
    keyctl  = true
  }
  disk {
    datastore_id = "local-lvm"
    size         = 16
  }
  operating_system {
    type = "debian"
    # Proxmox does not record the original template after creating a container.
    template_file_id = "storage:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
  }
  lifecycle {
    prevent_destroy = true
    ignore_changes  = [operating_system[0].template_file_id, disk]
  }
}
