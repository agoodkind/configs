locals {
  tack_data1_fdb = module.configuration.tack_fdb["tack_data1_suburban"]

  # Increment this value to download the overlay again with the current repository read token.
  tack_qa_repo_read_token_version = 1
}

module "tack_data1_fdb" {
  source = "../../modules/tack-fdb"

  providers = { docker = docker.tack_data1_suburban }

  node = local.guests["tack_data1_suburban"].node
  vmid = local.guests["tack_data1_suburban"].vmid
  kind = local.guests["tack_data1_suburban"].kind

  tack_commit                   = local.tack_release.commit
  overlay                       = local.tack_data1_fdb.overlay
  repository_read_token         = var.vault_tack_qa_repo_read_token
  repository_read_token_version = local.tack_qa_repo_read_token_version

  image             = local.tack_data1_fdb.image
  public_address    = local.tack_data1_fdb.public_address
  locality_id       = local.tack_data1_fdb.locality_id
  network           = local.tack_data1_fdb.network
  blobstore_address = local.tack_data1_fdb.blobstore_address
  volumes           = local.tack_data1_fdb.volumes

  depends_on = [module.base["tack_data1_suburban"], module.docker_guest["tack_data1_suburban"]]
}

import {
  for_each = local.tack_data1_fdb.volumes
  to       = module.tack_data1_fdb.docker_volume.fdb[each.key]
  id       = each.value.name
}

# Import the existing overlay file on the first Tack QA data guest. On the first apply after import,
# pveguest_download keeps the file without downloading it when its hash, mode, owner, and group match
# the configuration.
import {
  to = module.tack_data1_fdb.pveguest_download.overlay
  id = "${local.docker_guest_import_prefixes["tack_data1_suburban"]}/${local.tack_data1_fdb.overlay.path}"
}

# The plan replaces both imported containers to apply their environment, labels, and mounts.
# The replacements mount the existing volumes without deleting them.
import {
  to = module.tack_data1_fdb.docker_container.server
  id = "47916283488c912362c1a6e0c186da54915912c11cfd7ff754340f81c5c0eb2c"
}

import {
  to = module.tack_data1_fdb.docker_container.backup_agent
  id = "aba9bd1e96358432c49873ce58e1c3e2759570115dd19a600b829d75984e9670"
}
