# Cloudflare DNS records. Certificate automation manages ACME challenges separately.
locals {
  cloudflare_zone_ids = {
    "goodkind.io"      = "13ccc5ba064b1661495c0ec86511e9a7"
    "silver-flare.com" = "de02872854ceebbe1d731e3aef200de2"
  }

  cloudflare_static_dns_records = {
    "goodkind.io/A/*.home.goodkind.io/f078473b" = {
      zone    = "goodkind.io"
      name    = "*.home.goodkind.io"
      type    = "A"
      content = "10.250.0.110"
      ttl     = 1
      proxied = false
      comment = "proxy"
    }
    "goodkind.io/A/att-1335.goodkind.io/ae3d1c1d" = {
      zone    = "goodkind.io"
      name    = "att-1335.goodkind.io"
      type    = "A"
      content = "104.57.226.193"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/A/att4-1335.goodkind.io/18f8e10d" = {
      zone    = "goodkind.io"
      name    = "att4-1335.goodkind.io"
      type    = "A"
      content = "104.57.226.193"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/A/berylax.goodkind.io/96acbe4b" = {
      zone    = "goodkind.io"
      name    = "berylax.goodkind.io"
      type    = "A"
      content = "23.93.85.109"
      ttl     = 60
      proxied = false
    }
    "goodkind.io/A/hypervisor.home.goodkind.io/9f586dc5" = {
      zone    = "goodkind.io"
      name    = "hypervisor.home.goodkind.io"
      type    = "A"
      content = "10.250.0.254"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/A/hypervisor.suburban.goodkind.io/8f069cdc" = {
      zone    = "goodkind.io"
      name    = "hypervisor.suburban.goodkind.io"
      type    = "A"
      content = "10.240.0.148"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/A/jetkvm.suburban.goodkind.io/f61a6f29" = {
      zone    = "goodkind.io"
      name    = "jetkvm.suburban.goodkind.io"
      type    = "A"
      content = "10.240.0.48"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/A/mom6.suburban.goodkind.io/5fec0286" = {
      zone    = "goodkind.io"
      name    = "mom6.suburban.goodkind.io"
      type    = "A"
      content = "174.166.126.204"
      ttl     = 120
      proxied = false
    }
    "goodkind.io/A/router.suburban.goodkind.io/6161dd31" = {
      zone    = "goodkind.io"
      name    = "router.suburban.goodkind.io"
      type    = "A"
      content = "10.240.0.1"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/A/suburban-hypervisor.goodkind.io/60cc2139" = {
      zone    = "goodkind.io"
      name    = "suburban-hypervisor.goodkind.io"
      type    = "A"
      content = "10.240.0.148"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/A/unifi.home.goodkind.io/e1a87412" = {
      zone    = "goodkind.io"
      name    = "unifi.home.goodkind.io"
      type    = "A"
      content = "10.250.0.102"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/A/webpass-1335.goodkind.io/81d345af" = {
      zone    = "goodkind.io"
      name    = "webpass-1335.goodkind.io"
      type    = "A"
      content = "136.25.91.242"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/*.home.goodkind.io/9b9c74a5" = {
      zone    = "goodkind.io"
      name    = "*.home.goodkind.io"
      type    = "AAAA"
      content = "3d06:bad:b01::110"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/att-1335.goodkind.io/faea80b5" = {
      zone    = "goodkind.io"
      name    = "att-1335.goodkind.io"
      type    = "AAAA"
      content = "2600:1700:2f71:c80::1"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/att6-1335.goodkind.io/069a23bb" = {
      zone    = "goodkind.io"
      name    = "att6-1335.goodkind.io"
      type    = "AAAA"
      content = "2600:1700:2f71:c80::1"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/berylax.goodkind.io/71fc9381" = {
      zone    = "goodkind.io"
      name    = "berylax.goodkind.io"
      type    = "AAAA"
      content = "3d06:bad:b01:300::1"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/consul.home.goodkind.io/7872dc91" = {
      zone    = "goodkind.io"
      name    = "consul.home.goodkind.io"
      type    = "AAAA"
      content = "3d06:bad:b01::106"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/hypervisor.home.goodkind.io/ec2ffb07" = {
      zone    = "goodkind.io"
      name    = "hypervisor.home.goodkind.io"
      type    = "AAAA"
      content = "3d06:bad:b01::254"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/hypervisor.suburban.goodkind.io/98ab7c88" = {
      zone    = "goodkind.io"
      name    = "hypervisor.suburban.goodkind.io"
      type    = "AAAA"
      content = "3d06:bad:b01:200::1"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/mbrains6-1335.goodkind.io/dcd978bc" = {
      zone    = "goodkind.io"
      name    = "mbrains6-1335.goodkind.io"
      type    = "AAAA"
      content = "2607:f598:d3e8:3100::1"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/mini.home.goodkind.io/cfb5f2da" = {
      zone    = "goodkind.io"
      name    = "mini.home.goodkind.io"
      type    = "AAAA"
      content = "3d06:bad:b01:1:6e1f:f7ff:fe5b:f431"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/mom6.suburban.goodkind.io/98f56ad5" = {
      zone    = "goodkind.io"
      name    = "mom6.suburban.goodkind.io"
      type    = "AAAA"
      content = "2601:84:837c:a160:2030:94d9:51b1:497c"
      ttl     = 120
      proxied = false
    }
    "goodkind.io/AAAA/nas-jetkvm.goodkind.io/383f88d9" = {
      zone    = "goodkind.io"
      name    = "nas-jetkvm.goodkind.io"
      type    = "AAAA"
      content = "2607:f598:d3e0:131:3252:53ff:fe0d:6d08"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/nas.home.goodkind.io/238d3f5c" = {
      zone    = "goodkind.io"
      name    = "nas.home.goodkind.io"
      type    = "AAAA"
      content = "3d06:bad:b01:1::3"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/unifi.home.goodkind.io/3f88d2f2" = {
      zone    = "goodkind.io"
      name    = "unifi.home.goodkind.io"
      type    = "AAAA"
      content = "3d06:bad:b01::102"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/vault-jetkvm.goodkind.io/8d4b7e4f" = {
      zone    = "goodkind.io"
      name    = "vault-jetkvm.goodkind.io"
      type    = "AAAA"
      content = "2607:f598:d3e0:131:8234:28ff:fe66:5ed7"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/webpass-1335.goodkind.io/ad6aa736" = {
      zone    = "goodkind.io"
      name    = "webpass-1335.goodkind.io"
      type    = "AAAA"
      content = "2604:5500:c271:be00::1"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/webpass6-1335.goodkind.io/01dc891f" = {
      zone    = "goodkind.io"
      name    = "webpass6-1335.goodkind.io"
      type    = "AAAA"
      content = "2604:5500:c271:be00::1"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/CNAME/1335-sf.goodkind.io/a6e1c729" = {
      zone    = "goodkind.io"
      name    = "1335-sf.goodkind.io"
      type    = "CNAME"
      content = "lb-home.goodkind.io"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/66868087.goodkind.io/b29cef2e" = {
      zone    = "goodkind.io"
      name    = "66868087.goodkind.io"
      type    = "CNAME"
      content = "google.com"
      ttl     = 3600
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/blog.goodkind.io/f5a1060e" = {
      zone    = "goodkind.io"
      name    = "blog.goodkind.io"
      type    = "CNAME"
      content = "domains.tumblr.com"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/calendar.goodkind.io/2e106280" = {
      zone    = "goodkind.io"
      name    = "calendar.goodkind.io"
      type    = "CNAME"
      content = "ghs.googlehosted.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/cloudflared-opnsense-pkg.goodkind.io/21373f3e" = {
      zone    = "goodkind.io"
      name    = "cloudflared-opnsense-pkg.goodkind.io"
      type    = "CNAME"
      content = "public.r2.dev"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/clyde-suburban.goodkind.io/e50a47fc" = {
      zone    = "goodkind.io"
      name    = "clyde-suburban.goodkind.io"
      type    = "CNAME"
      content = "12761c69-2994-4cd0-a09a-a1f956995597.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/docs.goodkind.io/08ed1376" = {
      zone    = "goodkind.io"
      name    = "docs.goodkind.io"
      type    = "CNAME"
      content = "ghs.googlehosted.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/em805909.goodkind.io/35bd54ea" = {
      zone    = "goodkind.io"
      name    = "em805909.goodkind.io"
      type    = "CNAME"
      content = "return.smtp2go.net"
      ttl     = 3600
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/em805909.mail.goodkind.io/3b1cd28a" = {
      zone    = "goodkind.io"
      name    = "em805909.mail.goodkind.io"
      type    = "CNAME"
      content = "return.smtp2go.net"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/go.goodkind.io/c57232f1" = {
      zone    = "goodkind.io"
      name    = "go.goodkind.io"
      type    = "CNAME"
      content = "go-goodkind-io.pages.dev"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/goodkind.io/e000ba61" = {
      zone    = "goodkind.io"
      name    = "goodkind.io"
      type    = "CNAME"
      content = "goodkind-io.pages.dev"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/holy.goodkind.io/841fdde4" = {
      zone    = "goodkind.io"
      name    = "holy.goodkind.io"
      type    = "CNAME"
      content = "4b602332-6413-4f95-8874-561ed6d9b266.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/home-assistant-ext.goodkind.io/a948ecb4" = {
      zone    = "goodkind.io"
      name    = "home-assistant-ext.goodkind.io"
      type    = "CNAME"
      content = "4b602332-6413-4f95-8874-561ed6d9b266.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/home.goodkind.io/da2d9a8d" = {
      zone    = "goodkind.io"
      name    = "home.goodkind.io"
      type    = "CNAME"
      content = "lb-home.goodkind.io"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/link.goodkind.io/286ef534" = {
      zone    = "goodkind.io"
      name    = "link.goodkind.io"
      type    = "CNAME"
      content = "track.smtp2go.net"
      ttl     = 3600
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/link.mail.goodkind.io/bd7f86ba" = {
      zone    = "goodkind.io"
      name    = "link.mail.goodkind.io"
      type    = "CNAME"
      content = "track.smtp2go.net"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/mail.goodkind.io/e8b02507" = {
      zone    = "goodkind.io"
      name    = "mail.goodkind.io"
      type    = "CNAME"
      content = "ghs.googlehosted.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/mdm.goodkind.io/591778ce" = {
      zone    = "goodkind.io"
      name    = "mdm.goodkind.io"
      type    = "CNAME"
      content = "4b602332-6413-4f95-8874-561ed6d9b266.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/moto.goodkind.io/3fa38444" = {
      zone    = "goodkind.io"
      name    = "moto.goodkind.io"
      type    = "CNAME"
      content = "edge.sfo.the-cupcake-factory.com"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/plane.goodkind.io/16728285" = {
      zone    = "goodkind.io"
      name    = "plane.goodkind.io"
      type    = "CNAME"
      content = "4b602332-6413-4f95-8874-561ed6d9b266.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/router.128-nj.goodkind.io/50d22c45" = {
      zone    = "goodkind.io"
      name    = "router.128-nj.goodkind.io"
      type    = "CNAME"
      content = "router.suburban.goodkind.io"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/s805909._domainkey.goodkind.io/ad8cf231" = {
      zone    = "goodkind.io"
      name    = "s805909._domainkey.goodkind.io"
      type    = "CNAME"
      content = "dkim.smtp2go.net"
      ttl     = 3600
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/s805909._domainkey.mail.goodkind.io/09a5deb5" = {
      zone    = "goodkind.io"
      name    = "s805909._domainkey.mail.goodkind.io"
      type    = "CNAME"
      content = "dkim.smtp2go.net"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/sig1._domainkey.goodkind.io/008150cd" = {
      zone    = "goodkind.io"
      name    = "sig1._domainkey.goodkind.io"
      type    = "CNAME"
      content = "sig1.dkim.goodkind.io.at.icloudmailadmin.com"
      ttl     = 3600
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/suburban-pikvm.goodkind.io/03d0d234" = {
      zone    = "goodkind.io"
      name    = "suburban-pikvm.goodkind.io"
      type    = "CNAME"
      content = "6e73b6d4-2e0a-4a0c-b72f-b8b70d20f909.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/suburban.goodkind.io/0f8c1e70" = {
      zone    = "goodkind.io"
      name    = "suburban.goodkind.io"
      type    = "CNAME"
      content = "128-nj.goodkind.io"
      ttl     = 1
      proxied = false
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/vault-oob.goodkind.io/1df39f47" = {
      zone    = "goodkind.io"
      name    = "vault-oob.goodkind.io"
      type    = "CNAME"
      content = "88f11d0d-6148-4670-891d-72c0286ca48d.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/vault-test.goodkind.io/e7daee7a" = {
      zone    = "goodkind.io"
      name    = "vault-test.goodkind.io"
      type    = "CNAME"
      content = "50453c03-7d04-40fa-a86e-a8c88e851b78.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/CNAME/www.goodkind.io/7a96705b" = {
      zone    = "goodkind.io"
      name    = "www.goodkind.io"
      type    = "CNAME"
      content = "goodkind.io"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "goodkind.io/MX/goodkind.io/27b48ba4" = {
      zone     = "goodkind.io"
      name     = "goodkind.io"
      type     = "MX"
      content  = "alt4.aspmx.l.google.com"
      ttl      = 3600
      proxied  = false
      priority = 10
    }
    "goodkind.io/MX/goodkind.io/61881fa8" = {
      zone     = "goodkind.io"
      name     = "goodkind.io"
      type     = "MX"
      content  = "alt2.aspmx.l.google.com"
      ttl      = 3600
      proxied  = false
      priority = 5
    }
    "goodkind.io/MX/goodkind.io/67878bd2" = {
      zone     = "goodkind.io"
      name     = "goodkind.io"
      type     = "MX"
      content  = "aspmx.l.google.com"
      ttl      = 3600
      proxied  = false
      priority = 1
    }
    "goodkind.io/MX/goodkind.io/e6347368" = {
      zone     = "goodkind.io"
      name     = "goodkind.io"
      type     = "MX"
      content  = "alt3.aspmx.l.google.com"
      ttl      = 3600
      proxied  = false
      priority = 10
    }
    "goodkind.io/MX/goodkind.io/f8b773e7" = {
      zone     = "goodkind.io"
      name     = "goodkind.io"
      type     = "MX"
      content  = "alt1.aspmx.l.google.com"
      ttl      = 3600
      proxied  = false
      priority = 5
    }
    "goodkind.io/MX/old-email.goodkind.io/48d9fd5f" = {
      zone     = "goodkind.io"
      name     = "old-email.goodkind.io"
      type     = "MX"
      content  = "smtp.google.com"
      ttl      = 1
      proxied  = false
      priority = 1
    }
    "goodkind.io/TXT/_dmarc.goodkind.io/651a82c9" = {
      zone    = "goodkind.io"
      name    = "_dmarc.goodkind.io"
      type    = "TXT"
      content = "\"v=DMARC1; p=reject; rua=mailto:744241186d374281b7358391799e2867@dmarc-reports.cloudflare.net; adkim=s; aspf=s\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/_dmarc.mail.goodkind.io/0e48ce8b" = {
      zone    = "goodkind.io"
      name    = "_dmarc.mail.goodkind.io"
      type    = "TXT"
      content = "\"v=DMARC1; p=reject; rua=mailto:744241186d374281b7358391799e2867@dmarc-reports.cloudflare.net; adkim=s; aspf=s\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/_dmarc.old-email.goodkind.io/2bfe4762" = {
      zone    = "goodkind.io"
      name    = "_dmarc.old-email.goodkind.io"
      type    = "TXT"
      content = "\"v=DMARC1; p=none; rua=mailto:alex@goodkind.io\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/_gh-goodkind-io-o.goodkind.io/59c0868c" = {
      zone    = "goodkind.io"
      name    = "_gh-goodkind-io-o.goodkind.io"
      type    = "TXT"
      content = "\"11e97e5827\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/_github-challenge-alex-goodkind.goodkind.io/a1c947ef" = {
      zone    = "goodkind.io"
      name    = "_github-challenge-alex-goodkind.goodkind.io"
      type    = "TXT"
      content = "\"c4491162a2\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/goodkind.io/07cc8cf9" = {
      zone    = "goodkind.io"
      name    = "goodkind.io"
      type    = "TXT"
      content = "\"apple-domain=2G77rOfetj8hinJh\""
      ttl     = 3600
      proxied = false
    }
    "goodkind.io/TXT/goodkind.io/6f5f2057" = {
      zone    = "goodkind.io"
      name    = "goodkind.io"
      type    = "TXT"
      content = "\"google-site-verification=iYzYj6x-dZMA6HYJb7ltXEAAZrK6bnQDCVWQ-beoY3k\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/goodkind.io/7b7fae17" = {
      zone    = "goodkind.io"
      name    = "goodkind.io"
      type    = "TXT"
      content = "\"apple-domain=FHaOrA72yT5Sqqwr\""
      ttl     = 3600
      proxied = false
    }
    "goodkind.io/TXT/goodkind.io/8e67fd35" = {
      zone    = "goodkind.io"
      name    = "goodkind.io"
      type    = "TXT"
      content = "\"openai-domain-verification=dv-vPiafvSEi3m4eMj6Rr9ms38d\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/goodkind.io/94113210" = {
      zone    = "goodkind.io"
      name    = "goodkind.io"
      type    = "TXT"
      content = "\"v=spf1 include:_spf.google.com include:icloud.com ~all\""
      ttl     = 3600
      proxied = false
    }
    "goodkind.io/TXT/goodkind.io/c52ec6d9" = {
      zone    = "goodkind.io"
      name    = "goodkind.io"
      type    = "TXT"
      content = "\"google-gws-recovery-domain-verification=66868087\""
      ttl     = 3600
      proxied = false
    }
    "goodkind.io/TXT/google._domainkey.goodkind.io/66243211" = {
      zone    = "goodkind.io"
      name    = "google._domainkey.goodkind.io"
      type    = "TXT"
      content = "\"v=DKIM1; k=rsa;\\010p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAyxaYjul8sqCslBbGrqh6eymUyAviBQ7Vo+bu7IkKNHLY435ZAdKh40Kvo7MDXVNqbWkh9Gf2DB2DsEfAnJCKPJABw5/d7BUjF7N+/sMcUgh2jJk0J1oNi70hlPR43xp4ExMHm2MDPpLu5zCLEPncy0TFjtAkTaUE8uV0gT/C+l9fN6YLuKYEpQbaUAoyCvTtI\" \"/fSLJbFWn21JMLQKV9q1IIqUzQHzByhghJD6c1GAtnvPiWXY6ZxR+Tnl6aqmBOgX6eZ0oxrp9L0AsnawPgUMJ82CS2kGdK+oKJIGWgZbKjrdRiRWxb2A2lVXo2HopmbWjWW7+dvPzSEspszexqDAQIDAQAB\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/mail.goodkind.io/691f855d" = {
      zone    = "goodkind.io"
      name    = "mail.goodkind.io"
      type    = "TXT"
      content = "\"v=spf1 include:_spf.google.com include:spf.smtp2go.com -all\""
      ttl     = 1
      proxied = false
    }
    "goodkind.io/TXT/old-email.goodkind.io/e1a2311c" = {
      zone    = "goodkind.io"
      name    = "old-email.goodkind.io"
      type    = "TXT"
      content = "\"v=spf1 include:_spf.google.com include:icloud.com -all\""
      ttl     = 1
      proxied = false
    }
    "silver-flare.com/AAAA/alexs-mba.silver-flare.com/3b598473" = {
      zone    = "silver-flare.com"
      name    = "alexs-mba.silver-flare.com"
      type    = "AAAA"
      content = "3d06:bad:b01:cfcf::1"
      ttl     = 1
      proxied = false
      comment = "Private Mac address; reachable through organization-enrolled WARP clients."
    }
    "silver-flare.com/CNAME/alex-lm.silver-flare.com/ba0ae6b0" = {
      zone    = "silver-flare.com"
      name    = "alex-lm.silver-flare.com"
      type    = "CNAME"
      content = "7e78de31-0ae7-4ce5-9de8-5213e26d98f0.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "silver-flare.com/CNAME/clyde-adapter.silver-flare.com/ee0c9d35" = {
      zone    = "silver-flare.com"
      name    = "clyde-adapter.silver-flare.com"
      type    = "CNAME"
      content = "7e78de31-0ae7-4ce5-9de8-5213e26d98f0.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "silver-flare.com/CNAME/cursor.silver-flare.com/59864e77" = {
      zone    = "silver-flare.com"
      name    = "cursor.silver-flare.com"
      type    = "CNAME"
      content = "7e78de31-0ae7-4ce5-9de8-5213e26d98f0.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "silver-flare.com/CNAME/darwin-gh-broker.silver-flare.com/cc9ff380" = {
      zone    = "silver-flare.com"
      name    = "darwin-gh-broker.silver-flare.com"
      type    = "CNAME"
      content = "e52c8441-58df-400f-a230-af301c0c22a9.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "silver-flare.com/CNAME/pr-agent.goodkind.io.silver-flare.com/a9b69cc0" = {
      zone    = "silver-flare.com"
      name    = "pr-agent.goodkind.io.silver-flare.com"
      type    = "CNAME"
      content = "2cc52f61-fed1-480e-807e-6a273ce1ad0d.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "silver-flare.com/CNAME/pr-agent.silver-flare.com/8b38926a" = {
      zone    = "silver-flare.com"
      name    = "pr-agent.silver-flare.com"
      type    = "CNAME"
      content = "2cc52f61-fed1-480e-807e-6a273ce1ad0d.cfargotunnel.com"
      ttl     = 1
      proxied = true
      settings = {
        flatten_cname = false
      }
    }
    "silver-flare.com/MX/mail.silver-flare.com/03c41bd6" = {
      zone     = "silver-flare.com"
      name     = "mail.silver-flare.com"
      type     = "MX"
      content  = "route3.mx.cloudflare.net"
      ttl      = 1
      proxied  = false
      priority = 63
    }
    "silver-flare.com/MX/mail.silver-flare.com/1a018f31" = {
      zone     = "silver-flare.com"
      name     = "mail.silver-flare.com"
      type     = "MX"
      content  = "route2.mx.cloudflare.net"
      ttl      = 1
      proxied  = false
      priority = 88
    }
    "silver-flare.com/MX/mail.silver-flare.com/477ca396" = {
      zone     = "silver-flare.com"
      name     = "mail.silver-flare.com"
      type     = "MX"
      content  = "route1.mx.cloudflare.net"
      ttl      = 1
      proxied  = false
      priority = 54
    }
    "silver-flare.com/MX/silver-flare.com/34f6d277" = {
      zone     = "silver-flare.com"
      name     = "silver-flare.com"
      type     = "MX"
      content  = "route2.mx.cloudflare.net"
      ttl      = 1
      proxied  = false
      priority = 88
    }
    "silver-flare.com/MX/silver-flare.com/f377f59c" = {
      zone     = "silver-flare.com"
      name     = "silver-flare.com"
      type     = "MX"
      content  = "route1.mx.cloudflare.net"
      ttl      = 1
      proxied  = false
      priority = 54
    }
    "silver-flare.com/MX/silver-flare.com/f9eda4dc" = {
      zone     = "silver-flare.com"
      name     = "silver-flare.com"
      type     = "MX"
      content  = "route3.mx.cloudflare.net"
      ttl      = 1
      proxied  = false
      priority = 63
    }
    "silver-flare.com/TXT/cf2024-1._domainkey.silver-flare.com/253c2e5e" = {
      zone    = "silver-flare.com"
      name    = "cf2024-1._domainkey.silver-flare.com"
      type    = "TXT"
      content = "\"v=DKIM1; h=sha256; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAiweykoi+o48IOGuP7GR3X0MOExCUDY/BCRHoWBnh3rChl7WhdyCxW3jgq1daEjPPqoi7sJvdg5hEQVsgVRQP4DcnQDVjGMbASQtrY4WmB1VebF+RPJB2ECPsEDTpeiI5ZyUAwJaVX7r6bznU67g7LvFq35yIo4sdlmtZGV+i0H4cpYH9+3JJ78k\" \"m4KXwaf9xUJCWF6nxeD+qG6Fyruw1Qlbds2r85U9dkNDVAS3gioCvELryh1TxKGiVTkg4wqHTyHfWsp7KD3WQHYJn0RyfJJu6YEmL77zonn7p2SRMvTMP3ZEXibnC9gz3nnhR6wcYL8Q7zXypKTMD58bTixDSJwIDAQAB\""
      ttl     = 1
      proxied = false
    }
    "silver-flare.com/TXT/mail.silver-flare.com/55c4ec31" = {
      zone    = "silver-flare.com"
      name    = "mail.silver-flare.com"
      type    = "TXT"
      content = "\"v=spf1 include:_spf.mx.cloudflare.net ~all\""
      ttl     = 1
      proxied = false
    }
    "silver-flare.com/TXT/silver-flare.com/87c80d96" = {
      zone    = "silver-flare.com"
      name    = "silver-flare.com"
      type    = "TXT"
      content = "\"v=spf1 include:_spf.mx.cloudflare.net ~all\""
      ttl     = 1
      proxied = false
    }
  }

  cloudflare_dynamic_dns_records = {
    "goodkind.io/A/128-nj.goodkind.io/05f15b50" = {
      zone    = "goodkind.io"
      name    = "128-nj.goodkind.io"
      type    = "A"
      content = "174.166.126.204"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/128-nj6.goodkind.io/c492b155" = {
      zone    = "goodkind.io"
      name    = "128-nj6.goodkind.io"
      type    = "AAAA"
      content = "2601:84:837c:a160:f66d:4ff:fe66:b6de"
      ttl     = 1
      proxied = false
    }
    "goodkind.io/AAAA/hypervisor6.suburban.goodkind.io/40d44ad6" = {
      zone    = "goodkind.io"
      name    = "hypervisor6.suburban.goodkind.io"
      type    = "AAAA"
      content = "2601:84:837c:a160:f66d:4ff:fe66:b6de"
      ttl     = 1
      proxied = false
    }
  }
}

resource "cloudflare_dns_record" "static" {
  for_each = local.cloudflare_static_dns_records

  zone_id  = local.cloudflare_zone_ids[each.value.zone]
  name     = each.value.name
  type     = each.value.type
  content  = each.value.content
  ttl      = each.value.ttl
  proxied  = each.value.proxied
  priority = try(each.value.priority, null)
  comment  = try(each.value.comment, null)
  tags     = try(each.value.tags, null)
  settings = try(each.value.settings, null)

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_dns_record" "dynamic" {
  for_each = local.cloudflare_dynamic_dns_records

  zone_id = local.cloudflare_zone_ids[each.value.zone]
  name    = each.value.name
  type    = each.value.type
  content = each.value.content
  ttl     = each.value.ttl
  proxied = each.value.proxied

  lifecycle {
    prevent_destroy = true
    ignore_changes  = [content]
  }
}
