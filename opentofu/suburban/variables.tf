variable "ssh_keys" {
  description = "Newline-separated SSH public keys injected into new suburban guests."
  type        = string
}

variable "tack_qa_vm_id" {
  description = "Proxmox VMID for the tack-qa LXC on suburban."
  type        = number
}

variable "tack_qa_hostname" {
  description = "Bare hostname for the tack-qa LXC."
  type        = string
}

variable "tack_qa_ipv6_address" {
  description = "IPv6 address with prefix length assigned to tack-qa on vmbr1."
  type        = string
}

variable "tack_qa_ipv6_gateway" {
  description = "IPv6 gateway for tack-qa. The suburban host address on vmbr1."
  type        = string
}

variable "tack_qa_bridge" {
  description = "Proxmox bridge for the tack-qa NIC."
  type        = string
}

variable "tack_qa_mac_address" {
  description = "Stable MAC address for the tack-qa NIC."
  type        = string
}

variable "tack_qa_disk_size_gb" {
  description = "Root volume size in GB."
  type        = number
}

variable "tack_qa_memory_mb" {
  description = "Dedicated memory in MB."
  type        = number
}

variable "tack_qa_cpu_cores" {
  description = "Number of CPU cores allocated to the tack-qa LXC."
  type        = number
}

variable "tack_qa_tags" {
  description = "Proxmox tags applied to the tack-qa LXC."
  type        = list(string)
}

variable "tack_qa_template_file_id" {
  description = "LXC template used to provision tack-qa."
  type        = string
}

variable "tack_qa_datastore_id" {
  description = "Proxmox datastore for the tack-qa root volume."
  type        = string
}
