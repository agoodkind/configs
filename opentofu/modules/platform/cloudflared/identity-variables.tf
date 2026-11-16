variable "namespace" { type = string }
variable "connector_prefix" { type = string }
variable "manifest_prefix" { type = string }
variable "revision_label" { type = string }
variable "image" {
  type = string
  validation {
    condition     = can(regex("^.+@sha256:[a-f0-9]{64}$", var.image))
    error_message = "Supply an immutable cloudflared image with its SHA-256 digest."
  }
}
