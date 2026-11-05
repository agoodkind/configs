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
  idrac_host_artifact             = jsondecode(file("${path.module}/../idrac-certificate/host-artifact.auto.tfvars.json")).host_artifact
}

resource "pveguest_host_artifact" "idrac_certificate" {
  node            = "poweredge"
  workspace       = "poweredge/idrac-certificate"
  repository      = "agoodkind/configs"
  release_tag     = local.idrac_host_artifact.release_tag
  asset           = local.idrac_host_artifact.asset
  sha256          = local.idrac_host_artifact.sha256
  bundle_asset    = local.idrac_host_artifact.bundle_asset
  bundle_sha256   = local.idrac_host_artifact.bundle_sha256
  source_revision = local.idrac_host_artifact.source_revision
  program_bundles = local.idrac_host_artifact.program_bundles

  depends_on = [module.overlay]
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
  artifact_sha256      = pveguest_host_artifact.idrac_certificate.installed_sha256
  initial_installation = var.poweredge_idrac_initial_installation

  depends_on = [pveguest_host_artifact.idrac_certificate]
}
