# Tack QA LXC on suburban (VMID 103)
#
# QA environment for the Tack project management platform. Mirrors production
# CT 117 closely enough to validate wave migrations, seed runs, backfills, and
# restore drills before production is touched.
#
# Bridge: vmbr1 (suburban VM segment, 3d06:bad:b01:200::/64). IPv4 is not
# assigned; the Compose stack inside the LXC is forced IPv6-only, matching
# production posture. Access is via WireGuard (3d06:bad:b01::/56 is allowed
# by the vault peer on suburban).
#
# IPv6 suffix :1::117 keeps the production CT 117 mnemonic within the
# suburban VM sub-range (3d06:bad:b01:200:1::/80), clear of existing
# testbed allocations at ::100 and ::950.
#
# TACK-243

variable "tack_qa_vm_id" {
  description = "Proxmox VMID for the tack-qa LXC on suburban. Lowest free ID returned by pvesh get /cluster/nextid at plan time."
  type        = number
  default     = 103
}

variable "tack_qa_hostname" {
  description = "Bare hostname for the tack-qa LXC."
  type        = string
  default     = "tack-qa"
}

variable "tack_qa_ipv6_address" {
  description = "IPv6 address with prefix length assigned to tack-qa on vmbr1."
  type        = string
  default     = "3d06:bad:b01:200:1::117/64"
}

variable "tack_qa_ipv6_gateway" {
  description = "IPv6 gateway for tack-qa. The suburban host address on vmbr1."
  type        = string
  default     = "3d06:bad:b01:200::1"
}

variable "tack_qa_bridge" {
  description = "Proxmox bridge for the tack-qa NIC. vmbr1 is the suburban VM segment."
  type        = string
  default     = "vmbr1"
}

variable "tack_qa_mac_address" {
  description = "Stable MAC address for the tack-qa NIC. BC:24:11 OUI matches the OUI used by all LXC containers in this repo."
  type        = string
  default     = "BC:24:11:A3:53:17"
}

variable "tack_qa_disk_size_gb" {
  description = "Root volume size in GB. 40 GB provides headroom for one Yugabyte snapshot and one in-flight backup directory."
  type        = number
  default     = 40
}

variable "tack_qa_memory_mb" {
  description = "Dedicated memory in MB. Yugabyte alone uses ~3 GB at idle; FDB, Meilisearch, and the app fit in 8 GB."
  type        = number
  default     = 8192
}

variable "tack_qa_cpu_cores" {
  description = "Number of CPU cores allocated to the tack-qa LXC."
  type        = number
  default     = 2
}

variable "tack_qa_tags" {
  description = "Proxmox tags applied to the tack-qa LXC."
  type        = list(string)
  default     = ["lxc", "tack", "qa", "docker"]
}

variable "tack_qa_template_file_id" {
  description = "LXC template used to provision tack-qa. Must match the template available on suburban local storage."
  type        = string
  default     = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
}

variable "tack_qa_datastore_id" {
  description = "Proxmox datastore for the root volume. local-zfs is used by all suburban testbed LXCs and supports snapshots cleanly."
  type        = string
  default     = "local-zfs"
}

resource "proxmox_virtual_environment_container" "tack_qa" {
  provider  = proxmox.suburban
  node_name = "hypervisor"
  vm_id     = var.tack_qa_vm_id

  initialization {
    hostname = var.tack_qa_hostname
    ip_config {
      ipv6 {
        address = var.tack_qa_ipv6_address
        gateway = var.tack_qa_ipv6_gateway
      }
    }
    user_account {
      keys = [trimspace(data.http.github_ssh_keys.response_body)]
    }
  }

  features {
    # Nesting is required for Docker overlay filesystem inside LXC.
    nesting = true
  }

  network_interface {
    name        = "eth0"
    bridge      = var.tack_qa_bridge
    mac_address = var.tack_qa_mac_address
  }

  disk {
    datastore_id = var.tack_qa_datastore_id
    size         = var.tack_qa_disk_size_gb
  }

  memory { dedicated = var.tack_qa_memory_mb }
  cpu { cores = var.tack_qa_cpu_cores }

  tags = var.tack_qa_tags

  operating_system {
    template_file_id = var.tack_qa_template_file_id
    type             = "debian"
  }

  started      = true
  unprivileged = true

  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      # Proxmox does not persist the template name after provisioning;
      # ignoring this field prevents spurious drift on plan runs.
      operating_system[0].template_file_id,
    ]
  }
}
