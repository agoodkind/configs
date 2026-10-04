terraform {
  # `configsctl tofu` sets AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY from the
  # vault keys vault_tofu_env_AWS_ACCESS_KEY_ID and
  # vault_tofu_env_AWS_SECRET_ACCESS_KEY. The S3 backend rejects a sensitive
  # variable in this block.
  #
  # The endpoint host name includes the Cloudflare account id. The account id
  # is not a secret.
  backend "s3" {
    bucket = "tofu-state"
    key    = "opentofu.tfstate"
    region = "auto"

    endpoints = {
      s3 = "https://ee7d7ca7d611ef8c2a07885e8362de0c.r2.cloudflarestorage.com"
    }

    use_lockfile   = true
    use_path_style = true

    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_s3_checksum            = true
  }
}
