locals {
  hostname         = "idrac.poweredge.management.collingwood.goodkind.io"
  certificate_path = "/redfish/v1/Managers/iDRAC.Embedded.1/NetworkProtocol/HTTPS/Certificates"
}

resource "cloudflare_dns_record" "idrac" {
  zone_id = "13ccc5ba064b1661495c0ec86511e9a7"
  name    = local.hostname
  type    = "AAAA"
  content = "3d06:bad:b01:300::efb"
  ttl     = 300
  proxied = false
}

resource "restful_operation" "signing_request" {
  method = "POST"
  path   = "/redfish/v1/CertificateService/Actions/CertificateService.GenerateCSR"
  body = {
    CertificateCollection = local.certificate_path
    CommonName            = local.hostname
    AlternativeNames      = [local.hostname]
    # iDRAC requires nonempty subject fields in its signing request.
    # Let's Encrypt omits these fields from the issued certificate.
    City               = "Not applicable"
    State              = "Not applicable"
    Country            = "US"
    Organization       = "Not applicable"
    OrganizationalUnit = "Not applicable"
  }
  output_attrs = ["CSRString"]
}

resource "acme_registration" "idrac" {}

resource "acme_certificate" "idrac" {
  account_key_pem         = acme_registration.idrac.account_key_pem
  certificate_request_pem = restful_operation.signing_request.output.CSRString
  min_days_remaining      = 30
  cert_timeout            = 120

  dns_challenge {
    provider = "cloudflare"
  }

  depends_on = [cloudflare_dns_record.idrac]
}

resource "restful_operation" "install_certificate" {
  method = "POST"
  path   = "/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DelliDRACCardService/Actions/DelliDRACCardService.ImportSSLCertificate"
  body = {
    CertificateType    = "Server"
    SSLCertificateFile = "${acme_certificate.idrac.certificate_pem}\n${acme_certificate.idrac.issuer_pem}"
  }
}

resource "time_sleep" "web_service_restart" {
  create_duration = "60s"
  triggers = {
    certificate = sha256(acme_certificate.idrac.certificate_pem)
  }
  depends_on = [restful_operation.install_certificate]
}

data "http" "ready" {
  url                = "https://${local.hostname}/redfish/v1/"
  request_timeout_ms = 10000
  retry {
    attempts     = 10
    min_delay_ms = 3000
    max_delay_ms = 5000
  }
  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "iDRAC did not resume verified HTTPS after certificate installation."
    }
  }
  depends_on = [time_sleep.web_service_restart]
}

data "tls_certificate" "issued" {
  content = acme_certificate.idrac.certificate_pem
}

data "tls_certificate" "installed" {
  url          = "https://${local.hostname}"
  verify_chain = true
  lifecycle {
    postcondition {
      condition = (
        one([for certificate in self.certificates : certificate.cert_pem if !certificate.is_ca]) ==
        one([for certificate in data.tls_certificate.issued.certificates : certificate.cert_pem if !certificate.is_ca])
      )
      error_message = "iDRAC serves a different certificate from the certificate issued for this job."
    }
  }
  depends_on = [data.http.ready]
}

output "hostname" {
  value = local.hostname
}

output "valid_until" {
  value = one([for certificate in data.tls_certificate.issued.certificates : certificate.not_after if !certificate.is_ca])
}

output "certificate_pem_sha256" {
  value = sha256(one([for certificate in data.tls_certificate.issued.certificates : certificate.cert_pem if !certificate.is_ca]))
}
