variable "application_replicas" {
  description = "Number of Tack application Pods; a single replica updates without a surge."
  type        = number
  default     = 2
  validation {
    condition     = var.application_replicas >= 1 && floor(var.application_replicas) == var.application_replicas
    error_message = "Set application_replicas to a positive integer."
  }
}

variable "vm_capacity" {
  description = "CPU cores, memory in MiB, and root disk in GiB for the k3s VM."
  type = object({
    cores      = number
    memory_mib = number
    disk_gib   = number
  })
  default = { cores = 4, memory_mib = 8192, disk_gib = 40 }
}

variable "snapshots_enabled" {
  description = "Enable scheduled embedded etcd snapshots and their S3 destination."
  type        = bool
  default     = true
}
