terraform {
  required_providers {
    linux = {
      source  = "labbs/linux"
      version = "1.3.0"
    }
  }
}

variable "api_key" {
  type      = string
  sensitive = true
}

resource "linux_file" "smtp2go_env" {
  path = "/etc/send-email/smtp2go.env"
  mode = "0600"
  content = templatefile("${path.module}/smtp2go.env.tftpl", {
    api_key = var.api_key
  })
}
