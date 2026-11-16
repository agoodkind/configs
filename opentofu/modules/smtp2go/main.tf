terraform {
  required_providers {
    system = {
      source  = "neuspaces/system"
      version = "0.5.0"
    }
  }
}

variable "api_key" {
  type      = string
  sensitive = true
}

resource "system_folder" "send_email" {
  path = "/etc/send-email"
  mode = "755"
}

resource "system_file" "smtp2go_env" {
  path = "/etc/send-email/smtp2go.env"
  content_sensitive = templatefile("${path.module}/smtp2go.env.tftpl", {
    api_key = var.api_key
  })
  mode = "600"

  depends_on = [system_folder.send_email]
}
