module "tack_k3s" {
  source = "../../modules/tack-k3s"

  providers = { proxmox = proxmox.suburban }

  guest = merge(module.configuration.tack_k3s.guest, {
    bridge = module.suburban_config.bridges.trunk_suburban
  })
  cloud_image     = module.configuration.tack_k3s.cloud_image
  k3s_artifact    = module.configuration.tack_k3s.artifact
  cluster_network = module.configuration.tack_k3s.cluster_network
  snapshot        = module.configuration.tack_k3s.snapshot
  preparation = {
    public_keys                              = local.github_keys
    restricted_key_lines                     = local.sshpiper_lines
    timezone                                 = "America/Los_Angeles"
    login_dir                                = local.shared_vars.login_dir
    revision                                 = local.shared_vars.guest_prep_revision
    revision_file                            = local.shared_vars.guest_prep_revision_file
    scripts_updater_activation_run           = module.configuration.tack_k3s.scripts_updater_activation_run
    scripts_updater_randomized_delay_seconds = module.configuration.tack_k3s.scripts_updater_randomized_delay_seconds
  }
  snapshot_credentials = {
    access_key = trimspace(var.vault_seaweedfs_s3_access_key)
    secret_key = trimspace(var.vault_seaweedfs_s3_secret_key)
  }
  snapshot_credentials_version = module.configuration.tack_k3s.snapshot_credentials_version
  server_token                 = trimspace(var.vault_tack_qa_k3s_server_token)
  server_token_version         = module.configuration.tack_k3s.server_token_version

  application = {
    image         = "ghcr.io/agoodkind/tack-server@${local.tack_release.server_digest}"
    environment   = module.configuration.tack_k3s_application.environment
    ledger_hosts  = module.configuration.tack_k3s_application.ledger_hosts
    ledger_ca_pem = file("${path.module}/files/qa-ledger-ca.crt")
  }
  application_secrets = {
    database_url       = var.vault_tack_qa_k3s_database_url
    audit_operator_dsn = var.vault_tack_qa_k3s_audit_operator_dsn
    search_password    = var.vault_tack_qa_search_password
    search_cursor_key  = var.vault_tack_qa_search_cursor_key
    search_ca          = var.vault_tack_qa_search_ca
    fdb_bootstrap      = var.vault_tack_qa_k3s_fdb_bootstrap
  }
  application_secret_versions = module.configuration.tack_k3s_application.secret_versions
  registry_credentials = {
    username = "agoodkind"
    token    = var.vault_tack_qa_ghcr_pull_token
  }
  registry_credentials_version = module.configuration.tack_k3s_application.registry_credentials_version

  depends_on = [pveguest_systemd_unit.k3s_kafka_listener]
}

output "tack_k3s" {
  description = "QA k3s VM identity, addresses, and nonsecret cluster configuration."
  value = {
    vmid     = module.tack_k3s.vmid
    node     = module.tack_k3s.node
    hostname = module.tack_k3s.hostname
    ipv4     = module.tack_k3s.ipv4
    ipv6     = module.tack_k3s.ipv6
    cluster  = module.tack_k3s.cluster
  }
}
