resource "cloudflare_load_balancer_monitor" "http_monit_port_1406" {
  provider         = cloudflare.mwan_read
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

resource "cloudflare_load_balancer_monitor" "http_monit_port_1406_lossy" {
  provider         = cloudflare.mwan_read
  description      = "http-monit-port-1406-lossy"
  type             = "http"
  port             = 1406
  interval         = 60
  retries          = 5
  timeout          = 10
  expected_body    = ""
  expected_codes   = "200,404"
  follow_redirects = true
  allow_insecure   = false
  probe_zone       = ""
  path             = "/cf_check"
  method           = "HEAD"
  account_id       = var.cloudflare_account_id
  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_load_balancer_pool" "sf_1335_ipv6" {
  provider           = cloudflare.mwan_read
  description        = ""
  enabled            = true
  minimum_origins    = 1
  monitor            = cloudflare_load_balancer_monitor.http_monit_port_1406.id
  name               = "sf-1335-ipv6"
  notification_email = ""
  check_regions = [
    "WNAM"
  ]
  notification_filter = {
    pool = {}
  }
  origins = [
    {
      name          = "att6-1335"
      address       = "att6-1335.goodkind.io"
      enabled       = true
      weight        = 0.5
      flatten_cname = true
    },
    {
      name          = "webpass6-1335"
      address       = "webpass6-1335.goodkind.io"
      enabled       = true
      weight        = 0.5
      flatten_cname = true
    }
  ]
  account_id = var.cloudflare_account_id
  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_load_balancer_pool" "sf_att_1335" {
  provider           = cloudflare.mwan_read
  description        = ""
  enabled            = true
  minimum_origins    = 1
  monitor            = cloudflare_load_balancer_monitor.http_monit_port_1406.id
  name               = "sf-att-1335"
  notification_email = ""
  check_regions      = null
  notification_filter = {
    pool = {}
  }
  origins = [
    {
      name          = "att-1335"
      address       = "att-1335.goodkind.io"
      enabled       = true
      weight        = 1
      flatten_cname = true
    }
  ]
  account_id = var.cloudflare_account_id
  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_load_balancer_pool" "sf_mbrains6_1335" {
  provider           = cloudflare.mwan_read
  description        = ""
  enabled            = true
  minimum_origins    = 1
  monitor            = cloudflare_load_balancer_monitor.http_monit_port_1406_lossy.id
  name               = "sf-mbrains6-1335"
  notification_email = ""
  check_regions = [
    "WNAM"
  ]
  notification_filter = {
    pool = {}
  }
  origins = [
    {
      name          = "mbrains6-1335"
      address       = "mbrains6-1335.goodkind.io"
      enabled       = true
      weight        = 1
      flatten_cname = true
    }
  ]
  account_id = var.cloudflare_account_id
  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_load_balancer_pool" "sf_webpass_1335" {
  provider           = cloudflare.mwan_read
  description        = ""
  enabled            = true
  minimum_origins    = 1
  monitor            = cloudflare_load_balancer_monitor.http_monit_port_1406.id
  name               = "sf-webpass-1335"
  notification_email = ""
  check_regions      = null
  notification_filter = {
    pool = {}
  }
  origins = [
    {
      name          = "sf-webpass-1335"
      address       = "webpass-1335.goodkind.io"
      enabled       = true
      weight        = 1
      flatten_cname = true
    }
  ]
  account_id = var.cloudflare_account_id
  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_load_balancer" "lb_home6_goodkind_io" {
  provider         = cloudflare.mwan_read
  description      = ""
  ttl              = 10
  proxied          = false
  enabled          = true
  name             = "lb-home6.goodkind.io"
  session_affinity = "none"
  session_affinity_attributes = {
    samesite               = "Auto"
    secure                 = "Auto"
    drain_duration         = 0
    zero_downtime_failover = "none"
  }
  steering_policy = "random"
  fallback_pool   = cloudflare_load_balancer_pool.sf_mbrains6_1335.id
  default_pools = [
    cloudflare_load_balancer_pool.sf_1335_ipv6.id
  ]
  pop_pools    = {}
  region_pools = {}
  adaptive_routing = {
    failover_across_pools = false
  }
  random_steering = {
    default_weight = 1
  }
  location_strategy = {
    prefer_ecs = "proximity"
    mode       = "pop"
  }
  networks = [
    "cloudflare"
  ]
  zone_id = local.cloudflare_zone_ids["goodkind.io"]
  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_load_balancer" "lb_home_goodkind_io" {
  provider         = cloudflare.mwan_read
  description      = ""
  ttl              = 10
  proxied          = false
  enabled          = true
  name             = "lb-home.goodkind.io"
  session_affinity = "none"
  session_affinity_attributes = {
    samesite               = "Auto"
    secure                 = "Auto"
    drain_duration         = 0
    zero_downtime_failover = "none"
  }
  steering_policy = "random"
  fallback_pool   = cloudflare_load_balancer_pool.sf_mbrains6_1335.id
  default_pools = [
    cloudflare_load_balancer_pool.sf_att_1335.id,
    cloudflare_load_balancer_pool.sf_webpass_1335.id
  ]
  pop_pools    = {}
  region_pools = {}
  adaptive_routing = {
    failover_across_pools = true
  }
  random_steering = {
    default_weight = 1
  }
  location_strategy = {
    prefer_ecs = "never"
    mode       = "pop"
  }
  networks = [
    "cloudflare"
  ]
  zone_id = local.cloudflare_zone_ids["goodkind.io"]
  lifecycle {
    prevent_destroy = true
  }
}
