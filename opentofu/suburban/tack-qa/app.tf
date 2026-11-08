module "tack_app" {
  source = "../../modules/tack-app"

  providers = { docker = docker.tack_app2_suburban }

  commit        = local.tack_release.commit
  server_digest = local.tack_release.server_digest

  environment = module.configuration.tack_app.environment
  extra_hosts = module.configuration.tack_app.extra_hosts
  network     = module.configuration.tack_app.network
  log_volume  = module.configuration.tack_app.log_volume

  compose_writer_active = module.tack_release_app.compose_writer_active

  depends_on = [module.base["tack_app2_suburban"], module.docker_guest["tack_app2_suburban"], module.tack_release]
}

import {
  to = module.tack_app.docker_volume.logs
  id = "tack_tack-logs"
}

import {
  to = module.tack_app.docker_container.app
  id = "69f567bc87025aa9015c0823de140f2979caa11945431fed63b7819acc9205d5"
}
