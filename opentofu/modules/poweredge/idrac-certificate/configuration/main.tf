module "poweredge_host" {
  source = "../.."
}

output "lock_targets" {
  value = {
    module_hosts = {
      "restful_operation.signing_request"     = "poweredge"
      "restful_operation.install_certificate" = "poweredge"
    }
    lock_free_types = [
      "cloudflare_dns_record",
      "acme_registration",
      "acme_certificate",
      "time_sleep",
    ]
    lock_endpoints = {
      poweredge = module.poweredge_host.lock_endpoint
    }
  }
}
