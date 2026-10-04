# Logs in to a Proxmox host as root@pam with a password and a one-time code,
# and returns the session ticket. The Proxmox provider cannot do this login
# itself: its otp argument sends the code in the first request, and Proxmox
# expects it in a second request that answers a challenge.
terraform {
  required_providers {
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0"
    }
  }
}

variable "endpoint" {
  description = "Proxmox API base URL including port and a trailing slash"
  type        = string
}

variable "password" {
  description = "Password of root@pam"
  type        = string
  sensitive   = true
}

variable "otp" {
  description = "Current one-time code of root@pam"
  type        = string
  sensitive   = true
}

locals {
  username   = "root@pam"
  ticket_url = "${var.endpoint}api2/json/access/ticket"
  form_type  = "application/x-www-form-urlencoded"
}

# The first request returns a challenge ticket, not a session ticket.
data "http" "challenge" {
  url      = local.ticket_url
  method   = "POST"
  insecure = true

  request_headers = {
    Content-Type = local.form_type
  }
  request_body = "username=${urlencode(local.username)}&password=${urlencode(var.password)}"

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "Proxmox rejected the root@pam password at ${var.endpoint}."
    }
  }
}

locals {
  challenge = jsondecode(data.http.challenge.response_body).data
}

data "http" "session" {
  url      = local.ticket_url
  method   = "POST"
  insecure = true

  request_headers = {
    Content-Type        = local.form_type
    CSRFPreventionToken = local.challenge.CSRFPreventionToken
  }
  request_body = join("&", [
    "username=${urlencode(local.username)}",
    "tfa-challenge=${urlencode(local.challenge.ticket)}",
    "password=${urlencode("totp:${var.otp}")}",
  ])

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "Proxmox rejected the root@pam one-time code at ${var.endpoint}."
    }
  }
}

locals {
  session = jsondecode(data.http.session.response_body).data
}

output "auth_ticket" {
  description = "Session ticket for the Proxmox provider"
  value       = local.session.ticket
  sensitive   = true
}

output "csrf_prevention_token" {
  description = "CSRF token that belongs to the session ticket"
  value       = local.session.CSRFPreventionToken
  sensitive   = true
}
