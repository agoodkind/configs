resource "pveguest_host_artifact" "mwan_host" {
  node            = "suburban"
  workspace       = "suburban/mwan"
  repository      = "agoodkind/configs"
  release_tag     = var.host_artifact.release_tag
  asset           = var.host_artifact.asset
  sha256          = var.host_artifact.sha256
  bundle_asset    = var.host_artifact.bundle_asset
  bundle_sha256   = var.host_artifact.bundle_sha256
  source_revision = var.host_artifact.source_revision
  program_bundles = var.host_artifact.program_bundles
}

resource "pveguest_host_secret" "smtp2go" {
  node             = "suburban"
  workspace        = "suburban/mwan"
  field            = "SMTP2GO_API_KEY"
  value_wo         = var.vault_smtp2go_api_key
  value_wo_version = var.host_smtp2go_secret_version

  depends_on = [pveguest_host_artifact.mwan_host]
}
