variable "proxmox_api_token" {
  description = "Proxmox API token for the production vault host in the form user@pam!tokenid=secret"
  type        = string
  sensitive   = true
}

variable "proxmox_endpoint" {
  description = "Proxmox API base URL for the production vault host including port"
  type        = string
  default     = "https://[3d06:bad:b01::254]:8006/"
}

variable "suburban_proxmox_api_token" {
  description = "Proxmox API token for the suburban testbed host in the form user@pam!tokenid=secret"
  type        = string
  sensitive   = true
}

variable "suburban_proxmox_endpoint" {
  description = "Proxmox API base URL for the suburban testbed host including port"
  type        = string
  default     = "https://[3d06:bad:b01:200::1]:8006/"
}

variable "github_ssh_keys_user" {
  description = "GitHub username whose public keys are injected into new containers"
  type        = string
  default     = "agoodkind"
}

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
  description = "Dedicated memory in MB. Yugabyte alone uses about 3 GB at idle; FDB, Meilisearch, and the app fit in 8 GB."
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
