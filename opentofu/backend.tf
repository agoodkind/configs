terraform {
  # State lives in the Cloudflare R2 bucket "tofu-state". Credentials are never
  # written here: `configs tofu` injects them from the Ansible vault, and
  # opentofu/backend.md documents the contract. The endpoint embeds the
  # Cloudflare account id, which is stable and non-secret.
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
