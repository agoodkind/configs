terraform {
  required_version = ">= 1.11"
  required_providers {
    proxmox  = { source = "bpg/proxmox", version = "0.116.0" }
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}

resource "proxmox_download_file" "cloud_image" {
  node_name          = var.guest.node_name
  datastore_id       = var.guest.image_datastore
  content_type       = "import"
  file_name          = var.cloud_image.file_name
  url                = var.cloud_image.url
  checksum           = var.cloud_image.sha512
  checksum_algorithm = "sha512"
}

resource "proxmox_virtual_environment_file" "cloud_init" {
  node_name    = var.guest.node_name
  datastore_id = var.guest.snippet_datastore
  content_type = "snippets"

  source_raw {
    file_name = "tack-k3s-${var.guest.vmid}-cloud-init.yaml"
    data = "#cloud-config\n${yamlencode({
      hostname         = split(".", var.guest.hostname)[0]
      fqdn             = var.guest.hostname
      manage_etc_hosts = true
      disable_root     = false
      ssh_pwauth       = false
      users = ["default", {
        name                = "root"
        lock_passwd         = true
        ssh_authorized_keys = var.preparation.public_keys
      }]
      package_update = true
      packages       = ["qemu-guest-agent", "curl", "iptables", "conntrack"]
      runcmd         = [["systemctl", "enable", "--now", "qemu-guest-agent.service"]]
    })}"
  }
}

resource "proxmox_virtual_environment_vm" "server" {
  node_name     = var.guest.node_name
  vm_id         = var.guest.vmid
  name          = var.guest.hostname
  tags          = ["tack", "k3s"]
  started       = true
  on_boot       = true
  boot_order    = ["scsi0"]
  scsi_hardware = "virtio-scsi-single"

  cpu {
    cores = 4
    type  = "host"
  }
  memory { dedicated = 8192 }
  disk {
    datastore_id = var.guest.datastore_id
    import_from  = proxmox_download_file.cloud_image.id
    interface    = "scsi0"
    size         = 40
    discard      = "on"
    iothread     = true
  }
  network_device {
    bridge      = var.guest.bridge
    mac_address = var.guest.mac_address
    model       = "virtio"
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
    datastore_id      = var.guest.datastore_id
    user_data_file_id = proxmox_virtual_environment_file.cloud_init.id
    dns { servers = var.guest.dns_servers }
    ip_config {
      ipv4 {
        address = var.guest.ipv4_cidr
        gateway = var.guest.ipv4_gateway
      }
      ipv6 {
        address = var.guest.ipv6_cidr
        gateway = var.guest.ipv6_gateway
      }
    }
  }
  operating_system { type = "l26" }
  serial_device {}
}

module "base" {
  source = "../base"

  node = var.guest.node
  vmid = proxmox_virtual_environment_vm.server.vm_id
  kind = "qemu"
  name = var.guest.service_key

  public_keys                              = var.preparation.public_keys
  restricted_key_lines                     = var.preparation.restricted_key_lines
  timezone                                 = var.preparation.timezone
  login_dir                                = var.preparation.login_dir
  revision                                 = var.preparation.revision
  revision_file                            = var.preparation.revision_file
  scripts_updater_activation_run           = var.preparation.scripts_updater_activation_run
  scripts_updater_randomized_delay_seconds = var.preparation.scripts_updater_randomized_delay_seconds
}
