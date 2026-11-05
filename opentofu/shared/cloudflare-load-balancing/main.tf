resource "cloudflare_load_balancer_monitor" "http_monit_port_1406" {
  provider         = cloudflare.mwan_manage
  description      = "http-monit-port-1406"
  type             = "http"
  port             = 1406
  interval         = 60
  retries          = 2
  timeout          = 5
  expected_body    = ""
  expected_codes   = "200,404,204"
  follow_redirects = false
  allow_insecure   = false
  probe_zone       = ""
  path             = "/cf_check"
  method           = "HEAD"
  account_id       = var.cloudflare_account_id
  lifecycle {
    prevent_destroy = true
  }
}

module "configuration" { source = "../../modules/shared/cloudflare-load-balancing/configuration" }
