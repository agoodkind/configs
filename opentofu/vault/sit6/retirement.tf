locals {
  retirement_files = {
    library = {
      path    = "/usr/local/lib/configs/retire-routing-common.sh"
      content = file("${local.repository_root}/common/scripts/retire-routing-common.sh")
      mode    = "0644"
    }
    script = {
      path    = "/usr/local/sbin/retire-sit6-routing.sh"
      content = file("${path.module}/retire-routing.sh")
      mode    = "0755"
    }
    unit = {
      path    = "/etc/systemd/system/retire-sit6-routing.service"
      content = file("${path.module}/retire-routing.service")
      mode    = "0644"
    }
    forwarding = {
      path    = "/etc/sysctl.d/99-client-network.conf"
      content = file("${local.repository_root}/common/config/client-network.conf")
      mode    = "0644"
    }
  }
}

resource "pveguest_file" "retirement" {
  for_each = local.retirement_files

  node = "vault"
  vmid = local.service_mapping.sit6.vmid
  kind = "lxc"

  path    = each.value.path
  content = each.value.content
  mode    = each.value.mode
}

resource "pveguest_systemd_unit" "routing_retired" {
  node = "vault"
  vmid = local.service_mapping.sit6.vmid
  kind = "lxc"

  name    = "retire-sit6-routing.service"
  enabled = false
  active  = true

  restart_on = {
    for name, artifact in pveguest_file.retirement : name => artifact.write_id
  }
}
