locals {
  docker_guests = {
    tack_app2_suburban = {
      engine_versions = {
        "containerd.io"             = "2.3.4-2~debian.13~trixie"
        "docker-buildx-plugin"      = "0.37.0-1~debian.13~trixie"
        "docker-ce"                 = "5:29.8.0-1~debian.13~trixie"
        "docker-ce-cli"             = "5:29.8.0-1~debian.13~trixie"
        "docker-ce-rootless-extras" = "5:29.8.0-1~debian.13~trixie"
        "docker-compose-plugin"     = "5.5.1-1~debian.13~trixie"
      }
      repository = {
        suite        = "trixie"
        architecture = "amd64"
      }
      uplink_interface = "eth0"
      ndp_proxy_prefix = local.service_mapping.tack_app2_suburban.docker_v6_subnet
      sysctl_path      = "/etc/sysctl.d/99-tack-ipv6.conf"
    }
    tack_data1_suburban = {
      engine_versions = {
        "containerd.io"             = "2.3.4-2~debian.13~trixie"
        "docker-buildx-plugin"      = "0.37.0-1~debian.13~trixie"
        "docker-ce"                 = "5:29.8.0-1~debian.13~trixie"
        "docker-ce-cli"             = "5:29.8.0-1~debian.13~trixie"
        "docker-ce-rootless-extras" = "5:29.8.0-1~debian.13~trixie"
        "docker-compose-plugin"     = "5.5.1-1~debian.13~trixie"
      }
      repository = {
        suite        = "trixie"
        architecture = "amd64"
      }
      uplink_interface = "eth0"
      ndp_proxy_prefix = local.service_mapping.tack_data1_suburban.docker_v6_subnet
      sysctl_path      = "/etc/sysctl.d/99-tack-ipv6.conf"
    }
  }
}

output "docker_guests" { value = local.docker_guests }
