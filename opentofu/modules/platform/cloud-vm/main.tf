terraform {
  required_version = ">= 1.11"
  required_providers {
    proxmox = { source = "bpg/proxmox", version = "0.116.0" }
  }
}

resource "proxmox_download_file" "cloud_image" {
  count              = var.cloud_image == null ? 0 : 1
  node_name          = var.guest.node_name
  datastore_id       = var.guest.image_datastore
  content_type       = "import"
  file_name          = var.cloud_image.file_name
  url                = var.cloud_image.url
  checksum           = var.cloud_image.sha512
  checksum_algorithm = "sha512"
}

moved {
  from = proxmox_download_file.cloud_image
  to   = proxmox_download_file.cloud_image[0]
}

resource "proxmox_virtual_environment_file" "cloud_init" {
  node_name    = var.guest.node_name
  datastore_id = var.guest.snippet_datastore
  content_type = "snippets"

  source_raw {
    file_name = "${var.initialization.filename_prefix}-${var.guest.vmid}-cloud-init.yaml"
    data = "#cloud-config\n${yamlencode({
      hostname         = split(".", var.guest.hostname)[0]
      fqdn             = var.guest.hostname
      manage_etc_hosts = true
      disable_root     = false
      ssh_pwauth       = false
      users = ["default", {
        name                = "root"
        lock_passwd         = true
        ssh_authorized_keys = var.initialization.public_keys
      }]
      package_update = true
      packages       = var.initialization.packages
      runcmd         = [["systemctl", "enable", "--now", "qemu-guest-agent.service"]]
    })}"
  }
}

resource "proxmox_virtual_environment_vm" "server" {
  node_name     = var.guest.node_name
  vm_id         = var.guest.vmid
  name          = var.guest.hostname
  tags          = var.tags
  started       = var.started
  on_boot       = var.started
  boot_order    = ["scsi0"]
  scsi_hardware = "virtio-scsi-single"

  cpu {
    cores = var.vm_capacity.cores
    type  = "host"
  }
  memory { dedicated = var.vm_capacity.memory_mib }
  disk {
    datastore_id = var.guest.datastore_id
    import_from  = var.cloud_image == null ? var.shared_cloud_image_id : proxmox_download_file.cloud_image[0].id
    interface    = "scsi0"
    size         = var.vm_capacity.disk_gib
    discard      = "on"
    iothread     = true
  }
  dynamic "network_device" {
    for_each = var.guest.bridge == null ? [] : [var.guest.bridge]
    content {
      bridge      = network_device.value
      mac_address = var.guest.mac_address
      model       = "virtio"
    }
  }
  dynamic "hostpci" {
    for_each = var.pci_mapping == null ? [] : [var.pci_mapping]
    content {
      device  = "hostpci0"
      mapping = hostpci.value
    }
  }
  dynamic "hostpci" {
    for_each = var.secondary_pci_mapping == null ? [] : [var.secondary_pci_mapping]
    content {
      device  = "hostpci1"
      mapping = hostpci.value
    }
  }
  lifecycle {
    precondition {
      condition     = (var.guest.bridge == null) != (var.pci_mapping == null)
      error_message = "Select either a bridge or a PCI mapping for the guest network."
    }
  }
  agent {
    enabled = true
    timeout = "20m"
    wait_for_ip {
      ipv4 = true
      ipv6 = true
    }
  }
  initialization {
    datastore_id         = var.guest.datastore_id
    user_data_file_id    = proxmox_virtual_environment_file.cloud_init.id
    network_data_file_id = var.network_data_file_id
    dynamic "dns" {
      for_each = var.network_data_file_id == null ? [var.guest.dns_servers] : []
      content { servers = dns.value }
    }
    dynamic "ip_config" {
      for_each = var.guest.bridge == null || var.network_data_file_id != null ? [] : [var.guest]
      content {
        dynamic "ipv4" {
          for_each = var.guest.ipv4_cidr == null ? [] : [var.guest.ipv4_cidr]
          content {
            address = ipv4.value
            gateway = var.guest.ipv4_gateway
          }
        }
        ipv6 {
          address = var.guest.ipv6_cidr
          gateway = var.guest.ipv6_gateway
        }
      }
    }
  }
  operating_system { type = "l26" }
  serial_device {}
}
