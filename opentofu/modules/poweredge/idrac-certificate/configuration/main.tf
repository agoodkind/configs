module "poweredge_host" {
  source = "../.."
}

locals {
  runtime_root         = "/opt/configs/idrac-certificate"
  state_root           = "/var/lib/configs-idrac-certificate"
  configsctl_revision  = "d4572f95665c509f036622003c4706b018ba3e09"
  configsctl_sha256    = "309eed4ca1b05a69b7e1008bcaeb92ffe9c7f8fd626a200edb2c6b44f81f042e"
  tofu_sha256          = "8f95cbe1523ef7b7913773634a6d6ac94c38f3c8eadba2e18b1e5b01567561ad"
  artifact_workspace   = "poweredge/idrac-certificate"
  artifact_node        = "poweredge"
  artifact_environment = "testbed"
  module_hosts = {
    "restful_operation.signing_request"     = "poweredge"
    "restful_operation.install_certificate" = "poweredge"
  }
  lock_free_types = [
    "cloudflare_dns_record",
    "acme_registration",
    "acme_certificate",
    "time_sleep",
  ]
  configsctl_configuration = {
    tofu = {
      workspaces_dir = "workspace"
      env_key_prefix = "vault_tofu_env_"
      installed_source = {
        root        = local.runtime_root
        workspace   = local.artifact_workspace
        node        = local.artifact_node
        environment = local.artifact_environment
      }
      lock_targets = {
        workspace = {
          lock_endpoints  = { poweredge = { local = true } }
          module_hosts    = local.module_hosts
          lock_free_types = local.lock_free_types
        }
      }
    }
  }
}

output "runtime_root" { value = local.runtime_root }
output "state_root" { value = local.state_root }
output "configsctl_revision" { value = local.configsctl_revision }
output "configsctl_sha256" { value = local.configsctl_sha256 }
output "tofu_sha256" { value = local.tofu_sha256 }
output "service_mapping" { value = {} }
output "artifact_vars" { value = {
  idrac_artifact_node            = local.artifact_node
  idrac_artifact_environment     = local.artifact_environment
  idrac_configsctl_configuration = jsonencode(local.configsctl_configuration)
  idrac_binary_checksums         = "${local.configsctl_sha256}  ${local.runtime_root}/bin/configsctl\n${local.tofu_sha256}  ${local.runtime_root}/bin/tofu\n"
} }

output "lock_targets" {
  value = {
    module_hosts    = local.module_hosts
    lock_free_types = local.lock_free_types
    lock_endpoints = {
      poweredge = module.poweredge_host.lock_endpoint
    }
  }
}
