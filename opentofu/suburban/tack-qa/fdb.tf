locals { tack_data1_fdb = module.configuration.tack_fdb["tack_data1_suburban"] }

module "tack_data1_fdb" {
  source = "../../modules/tack-fdb"

  providers = { docker = docker.tack_data1_suburban }

  node = local.guests["tack_data1_suburban"].node
  vmid = local.guests["tack_data1_suburban"].vmid
  kind = local.guests["tack_data1_suburban"].kind

  image             = local.tack_data1_fdb.image
  public_address    = local.tack_data1_fdb.public_address
  locality_id       = local.tack_data1_fdb.locality_id
  network           = local.tack_data1_fdb.network
  blobstore_address = local.tack_data1_fdb.blobstore_address
  volumes           = local.tack_data1_fdb.volumes

  depends_on = [module.base["tack_data1_suburban"]]
}

import {
  for_each = local.tack_data1_fdb.volumes
  to       = module.tack_data1_fdb.docker_volume.fdb[each.key]
  id       = each.value.name
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
