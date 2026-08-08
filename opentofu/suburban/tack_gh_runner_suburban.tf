resource "proxmox_download_file" "tack_gh_runner_debian_cloud_image" {
  content_type       = "iso"
  datastore_id       = "local"
  file_name          = "debian-13-generic-amd64-20260803-2559.img"
  node_name          = "hypervisor"
  url                = "https://cloud.debian.org/images/cloud/trixie/20260803-2559/debian-13-generic-amd64-20260803-2559.qcow2"
  checksum           = "db3cd133207fc1d24b2488cfcbd5f6ddeadad028ae6f924928543448082aeab6e951c87d27eacd0470810e9fad977ef4e02a57e6e67b805397d4bbfb436b8b52"
  checksum_algorithm = "sha512"
}

resource "proxmox_virtual_environment_file" "tack_gh_runner_cloud_config" {
  content_type = "snippets"
  datastore_id = "local"
  node_name    = "hypervisor"

  source_raw {
    data = "#cloud-config\n${yamlencode({
      disable_root     = false
      hostname         = local.service_mapping.tack_gh_runner_suburban.hostname
      manage_etc_hosts = true
      package_update   = true
      packages         = ["qemu-guest-agent"]
      runcmd           = [["systemctl", "enable", "--now", "qemu-guest-agent"]]
      ssh_pwauth       = false
      users = [{
        lock_passwd         = true
        name                = "root"
        shell               = "/bin/bash"
        ssh_authorized_keys = split("\n", trimspace(var.ssh_keys))
      }]
    })}"
    file_name = "tack-gh-runner.cloud-config.yaml"
  }
}

resource "proxmox_virtual_environment_vm" "tack_gh_runner_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.tack_gh_runner_suburban.vmid
  name      = local.service_mapping.tack_gh_runner_suburban.hostname

  depends_on = [
    proxmox_network_linux_bridge.trunk_suburban,
  ]

  initialization {
    datastore_id = "local-zfs"

    ip_config {
      ipv6 {
        address = "${local.service_mapping.tack_gh_runner_suburban.ipv6}/64"
        gateway = local.service_mapping.opnsense_suburban.ipv6_vmnet
      }
    }
    dns {
      servers = [local.service_mapping.dns64_suburban.ipv6]
    }

    user_data_file_id = proxmox_virtual_environment_file.tack_gh_runner_cloud_config.id
  }

  agent {
    enabled = true
    type    = "virtio"

    wait_for_ip {
      ipv6 = true
    }
  }

  network_device {
    bridge      = proxmox_network_linux_bridge.trunk_suburban.name
    model       = "virtio"
    mac_address = local.service_mapping.tack_gh_runner_suburban.mac_address
  }

  disk {
    datastore_id = "local-zfs"
    discard      = "on"
    file_format  = "raw"
    file_id      = proxmox_download_file.tack_gh_runner_debian_cloud_image.id
    interface    = "scsi0"
    iothread     = true
    size         = 80
  }

  memory {
    dedicated = 12288
  }

  cpu {
    cores = 6
  }

  operating_system {
    type = "l26"
  }

  serial_device {
    device = "socket"
  }

  vga {
    type = "serial0"
  }

  boot_order    = ["scsi0"]
  machine       = "q35"
  on_boot       = true
  scsi_hardware = "virtio-scsi-pci"
  started       = true
  tags          = ["ci", "docker", "github-runner", "vm"]

  lifecycle {
    prevent_destroy = true
  }
}
