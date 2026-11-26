module "tack_k3s_vm" {
  source                = "../../modules/platform/cloud-vm"
  guest                 = module.configuration.tack_k3s.guest
  cloud_image           = module.configuration.tack_k3s.cloud_image
  vm_capacity           = module.configuration.vm_capacity
  tags                  = module.configuration.vm_recipe.tags
  pci_mapping           = proxmox_hardware_mapping_pci.tack.name
  secondary_pci_mapping = proxmox_hardware_mapping_pci.tack_secondary.name
  started               = !contains(var.stopped_guest_ids, local.tack_vf.vmid)
  initialization = {
    public_keys     = local.k3s_preparation.public_keys
    packages        = module.configuration.vm_recipe.packages
    filename_prefix = module.configuration.vm_recipe.filename_prefix
  }
}
locals { k3s_preparation = {
  public_keys                              = local.github_keys
  timezone                                 = module.configuration.vm_recipe.timezone
  login_dir                                = local.shared_vars.login_dir
  revision                                 = local.shared_vars.guest_prep_revision
  revision_file                            = local.shared_vars.guest_prep_revision_file
  scripts_updater_activation_run           = module.configuration.tack_k3s.scripts_updater_activation_run
  scripts_updater_randomized_delay_seconds = module.configuration.tack_k3s.scripts_updater_randomized_delay_seconds
} }
module "tack_k3s_base" {
  source = "../../modules/base"

  node = module.tack_k3s_vm.guest.node
  vmid = module.tack_k3s_vm.vmid
  kind = "qemu"
  name = module.tack_k3s_vm.guest.service_key

  networkd_wait_online_enabled = true

  public_keys                              = local.k3s_preparation.public_keys
  restricted_key_lines                     = []
  timezone                                 = local.k3s_preparation.timezone
  login_dir                                = local.k3s_preparation.login_dir
  revision                                 = local.k3s_preparation.revision
  revision_file                            = local.k3s_preparation.revision_file
  scripts_updater_activation_run           = local.k3s_preparation.scripts_updater_activation_run
  scripts_updater_randomized_delay_seconds = local.k3s_preparation.scripts_updater_randomized_delay_seconds
}

module "tack_k3s" {
  source = "../../modules/platform/k3s"

  guest             = module.tack_k3s_vm.guest
  k3s_artifact      = module.configuration.tack_k3s.artifact
  cluster_network   = module.configuration.tack_k3s.cluster_network
  snapshot          = module.configuration.tack_k3s.snapshot
  snapshots_enabled = module.configuration.snapshots_enabled
  snapshot_credentials = {
    access_key = trimspace(var.vault_seaweedfs_s3_access_key)
    secret_key = trimspace(var.vault_seaweedfs_s3_secret_key)
  }
  snapshot_credentials_version = module.configuration.tack_k3s.snapshot_credentials_version
  server_token                 = trimspace(var.vault_tack_k3s_server_token)
  server_token_version         = module.configuration.tack_k3s.server_token_version

  registry_credentials = {
    username = module.configuration.vm_recipe.registry_username
    token    = var.vault_tack_ghcr_pull_token
  }
  registry_credentials_version = module.configuration.tack_k3s_application.registry_credentials_version

  depends_on = [module.tack_k3s_base]
}
module "tack_application" {
  source               = "../../modules/tack/application"
  guest                = module.tack_k3s_vm.guest
  identity             = module.configuration.application_identity
  application_replicas = module.configuration.application_replicas
  application_strategy = module.configuration.application_strategy
  application = {
    image         = "ghcr.io/agoodkind/tack-server@${local.tack_release.server_digest}"
    environment   = module.configuration.tack_k3s_application.environment
    ledger_hosts  = module.configuration.tack_k3s_application.ledger_hosts
    ledger_ca_pem = file("${path.module}/files/prod-ledger-ca.crt")
  }
  application_secrets = {
    database_url       = var.vault_tack_k3s_database_url
    audit_operator_dsn = var.vault_tack_k3s_audit_operator_dsn
    search_password    = var.vault_tack_search_password
    search_cursor_key  = var.vault_tack_search_cursor_key
    search_ca          = var.vault_tack_search_ca
    fdb_bootstrap      = var.vault_tack_k3s_fdb_bootstrap
  }
  application_secret_versions = module.configuration.tack_k3s_application.secret_versions
  depends_on                  = [module.tack_k3s, module.tack_k3s_base, pveguest_systemd_unit.k3s_kafka_listener, pveguest_systemd_unit.search_firewall]
}

output "tack_k3s" {
  description = "Production k3s VM identity, addresses, and nonsecret cluster configuration."
  value = {
    vmid     = module.tack_k3s.vmid
    node     = module.tack_k3s.node
    hostname = module.tack_k3s.hostname
    ipv6     = module.tack_k3s.ipv6
    cluster  = module.tack_k3s.cluster
  }
}
