terraform {
  required_providers {
    restful = {
      source                = "magodo/restful"
      version               = "0.25.2"
      configuration_aliases = [restful.accounts]
    }
    remote = { source = "tenstad/remote", version = ">= 0.2.1" }
    tls    = { source = "hashicorp/tls", version = "4.1.0" }
  }
}

variable "ssh_host" { type = string }
variable "account_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "restful_operation" "account" {
  provider = restful.accounts
  method   = "PATCH"
  path     = "/redfish/v1/Managers/iDRAC.Embedded.1/Attributes"
  body = {
    Attributes = {
      "Users.5.UserName"  = "tofu-certificate"
      "Users.5.Enable"    = "Enabled"
      "Users.5.Privilege" = 3
    }
  }
  ephemeral_body = {
    Attributes = { "Users.5.Password" = var.account_password }
  }
}

resource "restful_operation" "usb" {
  method = "PATCH"
  path   = "/redfish/v1/Managers/iDRAC.Embedded.1/Attributes"
  body = {
    Attributes = {
      "OS-BMC.1.AdminState" = "Enabled"
      "OS-BMC.1.PTMode"     = "usb-p2p"
    }
  }
  depends_on = [restful_operation.account]
}
