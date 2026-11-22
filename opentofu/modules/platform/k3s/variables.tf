variable "guest" {
  description = "Proxmox VM identity, network addresses, and disk storage."
  type = object({
    node              = string
    node_name         = string
    vmid              = number
    hostname          = string
    service_key       = string
    bridge            = optional(string)
    mac_address       = string
    ipv4_cidr         = optional(string)
    ipv4_gateway      = optional(string)
    ipv6_cidr         = string
    ipv6_gateway      = optional(string)
    dns_servers       = list(string)
    datastore_id      = string
    image_datastore   = string
    snippet_datastore = string
  })

  validation {
    condition = (
      (var.guest.ipv4_cidr == null || can(cidrnetmask(var.guest.ipv4_cidr))) &&
      can(cidrhost(var.guest.ipv6_cidr, 0)) && strcontains(var.guest.ipv6_cidr, ":") &&
      length(var.guest.dns_servers) > 0
    )
    error_message = "Set a valid IPv6 CIDR and at least one DNS server for the VM. The optional IPv4 CIDR must be valid."
  }
}

variable "k3s_artifact" {
  description = "Immutable k3s version, binary URL, and published SHA-256 checksum."
  type = object({
    version = string
    url     = string
    sha256  = string
  })

  validation {
    condition = (
      can(regex("^v[0-9]+\\.[0-9]+\\.[0-9]+\\+k3s[0-9]+$", var.k3s_artifact.version)) &&
      startswith(var.k3s_artifact.url, "https://") &&
      can(regex("^[a-f0-9]{64}$", var.k3s_artifact.sha256))
    )
    error_message = "Set a stable k3s version, an HTTPS artifact URL, and its lowercase SHA-256 checksum."
  }
}

variable "cluster_network" {
  description = "Distinct IPv6-only Pod and Service CIDRs and the CoreDNS Service address."
  type = object({
    pod_cidr     = string
    service_cidr = string
    dns_address  = string
  })

  validation {
    condition = (
      can(cidrhost(var.cluster_network.pod_cidr, 0)) &&
      can(cidrhost(var.cluster_network.service_cidr, 0)) &&
      strcontains(var.cluster_network.pod_cidr, ":") &&
      strcontains(var.cluster_network.service_cidr, ":") &&
      !cidrcontains(var.cluster_network.pod_cidr, cidrhost(var.cluster_network.service_cidr, 0)) &&
      !cidrcontains(var.cluster_network.service_cidr, cidrhost(var.cluster_network.pod_cidr, 0)) &&
      cidrcontains(var.cluster_network.service_cidr, var.cluster_network.dns_address)
    )
    error_message = "Set nonoverlapping IPv6 Pod and Service CIDRs and a DNS address inside the Service CIDR."
  }
}

variable "snapshot" {
  description = "Existing S3 endpoint, bucket, isolated folder, optional CA PEM, and optional region."
  type = object({
    endpoint = string
    bucket   = string
    folder   = string
    ca_pem   = optional(string)
    region   = optional(string)
    insecure = optional(bool, false)
  })
  default = null

  validation {
    condition = var.snapshot == null ? !var.snapshots_enabled : (
      length(trimspace(var.snapshot.endpoint)) > 0 &&
      !strcontains(var.snapshot.endpoint, "://") &&
      length(trimspace(var.snapshot.bucket)) > 0 &&
      length(trim(var.snapshot.folder, "/")) > 0
    )
    error_message = "Set the existing S3 endpoint as host:port without a scheme, bucket, and isolated folder."
  }
}

variable "snapshot_credentials" {
  description = "Ephemeral S3 credentials from the encrypted credential source."
  type = object({
    access_key = string
    secret_key = string
  })
  sensitive = true
  ephemeral = true
  default   = null

  validation {
    condition     = var.snapshot_credentials == null ? !var.snapshots_enabled : (length(var.snapshot_credentials.access_key) > 0 && length(var.snapshot_credentials.secret_key) > 0)
    error_message = "Supply both S3 access and secret keys from the protected credential source."
  }
}

variable "snapshot_credentials_version" {
  description = "Increment this version when the protected S3 credentials change."
  type        = number
  default     = 1

  validation {
    condition     = var.snapshot_credentials_version >= 1 && floor(var.snapshot_credentials_version) == var.snapshot_credentials_version
    error_message = "Set snapshot_credentials_version to a positive integer."
  }
}

variable "server_token" {
  description = "Ephemeral k3s server token preserved separately in the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true

  validation {
    condition     = length(trimspace(var.server_token)) >= 32 && !strcontains(trimspace(var.server_token), "\n")
    error_message = "Supply the preserved k3s server token with at least 32 characters on one line."
  }
}

variable "server_token_version" {
  description = "Increment this version when the protected server token file changes."
  type        = number

  validation {
    condition     = var.server_token_version >= 1 && floor(var.server_token_version) == var.server_token_version
    error_message = "Set server_token_version to a positive integer."
  }
}
variable "snapshots_enabled" {
  description = "Enable scheduled embedded etcd snapshots and their S3 destination."
  type        = bool
}
