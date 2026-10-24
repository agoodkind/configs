# Router-2 simulator: an FRR speaker on the transit link that proves router
# N+1 onboarding needs no MWAN change. One interface, on the transit bridge;
# the announced prefix lives on its loopback, configured by
# deploy-router2-sim.yml through pct alongside FRR itself.
resource "proxmox_virtual_environment_container" "router2_suburban" {
  node_name = "hypervisor"
  vm_id     = local.service_mapping.router2_suburban.vmid


  initialization {
    hostname = local.service_mapping.router2_suburban.hostname
    # Public resolvers, matching the ISP sims. The transit link is the
    # router's WAN side, so this guest cannot reach the guest-segment
    # resolver through it.
    dns {
      servers = ["2606:4700:4700::1111", "1.1.1.1"]
    }
    # Transit link, where it peers BGP. No gateway here: the router does not
    # forward this guest to the guest segment or the internet, so egress
    # rides the management uplink below.
    ip_config {
      ipv4 {
        address = "${local.service_mapping.router2_suburban.ipv4_transit}/29"
      }
      ipv6 {
        address = "${local.service_mapping.router2_suburban.ipv6_transit}/64"
      }
    }
    ip_config {
      ipv4 {
        address = "${local.service_mapping.router2_suburban.ipv4_uplink}/24"
        gateway = local.service_mapping.vmbr1_suburban.ipv4
      }
      ipv6 {
        address = "${local.service_mapping.router2_suburban.ipv6_uplink}/64"
        gateway = local.service_mapping.vmbr1_suburban.ipv6
      }
    }
    user_account {
      keys = [var.ssh_keys]
    }
  }

  # No features block. FRR needs none of the advanced container features, and
  # Proxmox writes no features line when every flag is off. Declaring one here
  # makes the provider send fuse, keyctl and mknod on create, which Proxmox
  # refuses for any actor but root@pam on a privileged container.

  network_interface {
    name        = "eth0"
    bridge      = var.host_networks.mwan_suburban
    mac_address = "BC:24:11:02:09:05"
  }

  network_interface {
    name        = "eth1"
    bridge      = var.host_networks.vm_management_suburban
    mac_address = "BC:24:11:02:09:93"
  }

  disk {
    datastore_id = "local-zfs"
    size         = 4
  }

  memory {
    dedicated = 512
    swap      = 512
  }

  cpu {
    architecture = "amd64"
    cores        = 1
    limit        = 0
  }

  console {
    enabled   = true
    tty_count = 2
    type      = "tty"
  }

  tags = []

  operating_system {
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    type             = "debian"
  }

  started       = true
  start_on_boot = true
  unprivileged  = true

  # The simulator holds no state worth protecting: its FRR config is rendered
  # by its deploy play, so recreating it costs one deploy.
  lifecycle {
    prevent_destroy = false
    ignore_changes = [
      initialization[0].user_account,
      operating_system[0].template_file_id,
    ]
  }
}

variable "service_mapping" { type = any }

variable "ssh_keys" { type = string }

variable "host_networks" { type = map(string) }

locals { service_mapping = var.service_mapping }

terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.106.0"

    }
  }
}
