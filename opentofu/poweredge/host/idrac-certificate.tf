variable "poweredge_idrac_setup_endpoint" {
  description = "Verified iDRAC origin for account and USB configuration."
  type        = string
  default     = "https://idrac.poweredge.management.collingwood.goodkind.io"
}

variable "poweredge_idrac_initial_ca_file" {
  description = "Pinned factory certificate file used only for initial installation."
  type        = string
  default     = ""
}

variable "vault_poweredge_idrac_rotator_password" {
  description = "Ephemeral password of the existing iDRAC account administrator."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_poweredge_idrac_certificate_password" {
  description = "Ephemeral password of the dedicated certificate account."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "poweredge_idrac_initial_installation" {
  description = "Install the temporary factory-certificate connection for the first certificate request."
  type        = bool
  default     = false
}

locals {
  idrac_root_ca_certificate_files = var.poweredge_idrac_initial_ca_file == "" ? null : [var.poweredge_idrac_initial_ca_file]
}

provider "restful" {
  alias    = "idrac_accounts"
  base_url = var.poweredge_idrac_setup_endpoint
  client = {
    certificates              = []
    tls_insecure_skip_verify  = false
    root_ca_certificate_files = local.idrac_root_ca_certificate_files
  }
  security = {
    http = {
      basic = {
        username = "tofu-rotator"
        password = var.vault_poweredge_idrac_rotator_password
      }
    }
  }
}

provider "restful" {
  alias    = "idrac_manager"
  base_url = var.poweredge_idrac_setup_endpoint
  client = {
    certificates              = []
    tls_insecure_skip_verify  = false
    root_ca_certificate_files = local.idrac_root_ca_certificate_files
  }
  security = {
    http = {
      basic = {
        username = "tofu-certificate"
        password = var.vault_poweredge_idrac_certificate_password
      }
    }
  }
}

module "idrac_certificate" {
  source = "../../modules/poweredge/idrac-certificate"
  providers = {
    restful          = restful.idrac_manager
    restful.accounts = restful.idrac_accounts
  }
  ssh_host             = local.service_mapping.poweredge_hypervisor.ipv4
  account_password     = var.vault_poweredge_idrac_certificate_password
  workspace_directory  = abspath("${path.module}/../idrac-certificate")
  factory_ca_file      = "${path.module}/../firmware/idrac-ca.pem"
  initial_installation = var.poweredge_idrac_initial_installation
}
