locals {
  state_backend = jsondecode(file("${path.root}/../../modules/contracts/state-backend.json"))
}

terraform {
  backend "s3" {
    bucket = local.state_backend.bucket
    key    = "poweredge/host.tfstate"
    region = local.state_backend.region

    endpoints = local.state_backend.endpoints

    use_lockfile   = local.state_backend.use_lockfile
    use_path_style = local.state_backend.use_path_style

    skip_credentials_validation = local.state_backend.skip_credentials_validation
    skip_region_validation      = local.state_backend.skip_region_validation
    skip_requesting_account_id  = local.state_backend.skip_requesting_account_id
    skip_metadata_api_check     = local.state_backend.skip_metadata_api_check
    skip_s3_checksum            = local.state_backend.skip_s3_checksum
  }
}
