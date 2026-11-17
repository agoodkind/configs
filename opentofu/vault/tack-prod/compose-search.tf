provider "linux" {
  alias = "compose"
  ssh   = { host = local.service_mapping[module.configuration.search_service_keys.compose].ipv6 }
}

resource "linux_file_line" "compose_search" {
  provider          = linux.compose
  path              = "/root/tack/.env"
  match             = "^OPENSEARCH_ENDPOINT="
  line              = "OPENSEARCH_ENDPOINT=${module.configuration.tack_k3s_application.environment.OPENSEARCH_ENDPOINT}"
  remove_on_destroy = false
  depends_on        = [pveguest_systemd_unit.search_firewall]
}
