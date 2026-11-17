resource "linux_file" "search_proxy_retirement_rules" {
  path    = "/usr/local/share/tack/retire-search-proxy.conf"
  content = file("${path.module}/search/retire-search-proxy.conf")
}

resource "linux_file" "search_proxy_retirement_unit" {
  path    = "/etc/systemd/system/retire-tack-search-proxy.service"
  content = file("${path.module}/search/retire-tack-search-proxy.service")
}

resource "linux_systemd_unit" "retire_search_proxy" {
  name          = "retire-tack-search-proxy.service"
  active        = true
  daemon_reload = true
  triggers = {
    unit  = linux_file.search_proxy_retirement_unit.sha256
    rules = linux_file.search_proxy_retirement_rules.sha256
  }
}
