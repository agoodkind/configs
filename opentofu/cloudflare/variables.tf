variable "account_id" {
  description = "Cloudflare account that owns the Zero Trust objects in this module."
  type        = string
}

variable "owner_email" {
  description = "Identity the device profiles match on. Every profile applies only to this user."
  type        = string
}

variable "berylax_beacon_sockaddr" {
  description = <<-EOT
    Host and port of the TLS endpoint that proves a device sits on the Berylax LAN.
    The Cloudflare One Client opens TLS here and compares the certificate against
    berylax_beacon_sha256. Cloudflare excludes this address from every device
    profile, so it must be an address no remote user needs. An IPv6 endpoint does
    not work: the client rejects the bracketed literal before it opens a socket,
    and the API rejects the unbracketed form.
  EOT
  type        = string
}

variable "berylax_beacon_sha256" {
  description = "SHA-256 fingerprint of the certificate served at berylax_beacon_sockaddr."
  type        = string
}

variable "dns_search_suffix" {
  description = "DNS search suffix pushed to devices by every profile."
  type        = string
}

variable "berylax_include" {
  description = <<-EOT
    Split-tunnel include list for the on-LAN Berylax profile, in the exact order
    Cloudflare stores it. The order is load bearing, because the provider diffs
    this list by position. A list in a different order makes the provider send one
    entry holding both an address and a host, and the API rejects that with
    error 2049.
  EOT
  type = list(object({
    address     = optional(string)
    host        = optional(string)
    description = optional(string)
  }))
}

variable "tun_only_include" {
  description = <<-EOT
    Split-tunnel include list for the away profile, in the exact order
    Cloudflare stores it. The provider diffs this list by position. A reordered
    list can make it send one entry with both an address and a host, which the
    API rejects with error 2049.
  EOT
  type = list(object({
    address     = optional(string)
    host        = optional(string)
    description = optional(string)
  }))
}
