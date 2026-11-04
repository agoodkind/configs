output "suburban_vars" { value = {
  "mwan_routed_ipv4"     = "${local.service_mapping.mwan_suburban.ipv4_routed}/24"
  "mwan_internal_prefix" = "3d06:bad:b01:210::/60"
  "mwan_providers" = [{
    "name" = "att"
    "ipv4" = {
      "translation" = {
        "static_mappings" = [{
          "external" = "10.241.205.2"
          }, {
          "external" = "10.241.205.3"
          }, {
          "external" = "10.241.205.4"
          }, {
          "external" = "10.241.205.5"
          }, {
          "external" = "10.241.205.6"
        }]
      }
    }
    "ipv6" = {
      "translation" = {
        "nptv6" = {
          "expected_prefix" = "3d06:bad:b01:2300::/60"
        }
      }
    }
    }, {
    "name" = "webpass"
    "ipv4" = {
      "translation" = {
        "static_mappings" = [{
          "external" = "10.241.204.2"
          }, {
          "external" = "10.241.204.3"
          }, {
          "external" = "10.241.204.4"
          }, {
          "external" = "10.241.204.5"
          }, {
          "external" = "10.241.204.6"
        }]
      }
    }
    "ipv6" = {
      "translation" = {
        "nptv6" = {
          "expected_prefix" = "3d06:bad:b01:2200::/60"
        }
      }
    }
    }, {
    "name" = "routed"
    "ipv6" = {
      "address" = "${local.service_mapping.mwan_suburban.ipv6_routed}/64"
    }
  }]
} }
