# This root does not have a backend, providers, or resources. CI plans it
# to produce the nonsecret input projection for python -m hostartifact build.
# configsctl does not select it as a workspace because it does not have a
# backend block.
locals {
  repository_root = abspath("${path.module}/../../..")
}

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

module "targets" {
  source          = "../../modules/host-artifact-targets"
  repository_root = local.repository_root
  workspace       = var.workspace
}
