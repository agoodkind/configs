locals {
  shared_vars = yamldecode(
    file("${path.module}/../ansible/inventory/group_vars/all/vars.yml")
  )
}

data "http" "github_ssh_keys" {
  url = "https://github.com/${local.shared_vars.github_ssh_keys_user}.keys"

  lifecycle {
    postcondition {
      condition = self.status_code == 200 && length([
        for line in split("\n", replace(self.response_body, "\r", "")) : line
        if trimspace(line) != ""
        ]) > 0 && alltrue([
        for line in split("\n", replace(self.response_body, "\r", "")) :
        can(regex(
          "^(ssh-[A-Za-z0-9@._+-]+|ecdsa-[A-Za-z0-9@._+-]+|sk-[A-Za-z0-9@._+-]+) [A-Za-z0-9+/]+={0,3}( .*)?$",
          trimspace(line),
        ))
        if trimspace(line) != ""
      ])
      error_message = "GitHub returned an invalid SSH public key response for ${local.shared_vars.github_ssh_keys_user}."
    }
  }
}

module "suburban" {
  source = "./suburban"

  providers = {
    proxmox      = proxmox.suburban
    proxmox.root = proxmox.suburban_root
  }

  ssh_keys = trimspace(data.http.github_ssh_keys.response_body)
}

module "vault" {
  source = "./vault"

  providers = {
    proxmox = proxmox
  }

  ssh_keys = trimspace(data.http.github_ssh_keys.response_body)
}

module "cloudflare" {
  source = "./cloudflare"

  providers = {
    cloudflare = cloudflare
  }

  account_id  = var.cloudflare_account_id
  owner_email = var.cloudflare_owner_email

  berylax_beacon_sockaddr = var.berylax_beacon_sockaddr
  berylax_beacon_sha256   = var.berylax_beacon_sha256

  dns_search_suffix = "home.goodkind.io"

  # Both lists below are in the order Cloudflare stores them. The provider diffs
  # a split-tunnel list by position, so reordering an entry makes it send one
  # object holding both an address and a host, which the API rejects.
  #
  # The Berylax list still splits the site prefix into eight fragments that add up
  # to the /48 minus 3d06:bad:b01:300::/56. Rewriting it as explicit /56 entries
  # is a content change, so it stays out of this import.
  berylax_include = [
    { address = "3eef::/48", description = "suburban testbed v6" },
    { address = "3d06:bad:b01::/55", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:800::/53", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:8000::/49", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:400::/54", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:4000::/50", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:200::/56", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:2000::/51", description = "v6-all except Berylax LAN /56" },
    { address = "3d06:bad:b01:1000::/52", description = "v6-all except Berylax LAN /56" },
    { address = "100.96.0.0/12", description = "WARP device IPs" },
    { address = "10.250.0.0/16", description = "home-v4" },
    { address = "10.240.0.0/16", description = "suburban-v4" },
    { host = "*.cloudflareaccess.com" },
  ]

  tun_only_include = [
    { address = "3d06:bad:b01::/56", description = "home" },
    { address = "3d06:bad:b01:6400::/56", description = "production NAT64" },
    { address = "3d06:bad:b01:300::/56", description = "berylax" },
    { address = "3d06:bad:b01:2600::/56", description = "suburban NAT64" },
    { address = "3d06:bad:b01:2400::/56", description = "monkeybrains sim NPTv6 egress" },
    { address = "3d06:bad:b01:2300::/56", description = "att sim NPTv6 egress" },
    { address = "3d06:bad:b01:2200::/56", description = "webpass sim NPTv6 egress" },
    { address = "3d06:bad:b01:200::/56", description = "suburban" },
    { address = "100.96.0.0/12", description = "WARP device IPs" },
    { address = "10.250.0.0/16", description = "home-v4" },
    { address = "10.240.0.0/16", description = "suburban-v4" },
    { host = "*.cloudflareaccess.com" },
  ]
}
