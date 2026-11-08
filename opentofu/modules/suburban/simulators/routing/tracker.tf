variable "route_trackers" {
  type = map(object({
    destination       = string
    gateway           = string
    interval_seconds  = number
    failure_threshold = number
    route_metric      = number
  }))
  default = {}

  validation {
    condition     = alltrue([for key in keys(var.route_trackers) : contains(var.guest_keys, key)])
    error_message = "Each route_trackers key must be a guest key in guest_keys."
  }

  validation {
    condition = alltrue([
      for key, tracker in var.route_trackers :
      length([for tunnel in var.network[key].tunnels : tunnel if tunnel.peer_inner_ipv6 == tracker.gateway]) == 1
    ])
    error_message = "Each route_trackers gateway must equal the peer_inner_ipv6 of exactly one tunnel of its guest."
  }
}

locals {
  tracker_script_path = "/usr/local/sbin/routing-home-route-tracker"
  tracker_unit_path   = "/etc/systemd/system/routing-home-route-0.service"
  tracker_unit_name   = "routing-home-route-0.service"
}

resource "pveguest_file" "route_tracker_script" {
  for_each = var.route_trackers

  node = local.routing_guests[each.key].node
  vmid = local.routing_guests[each.key].vmid
  kind = local.routing_guest_kind

  path    = local.tracker_script_path
  content = file("${path.module}/files/home-route-tracker.sh")
  mode    = "0755"

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}

resource "pveguest_file" "route_tracker_unit" {
  for_each = var.route_trackers

  node = local.routing_guests[each.key].node
  vmid = local.routing_guests[each.key].vmid
  kind = local.routing_guest_kind

  path = local.tracker_unit_path
  content = templatefile("${path.module}/files/home-route-tracker.service.tftpl", {
    destination       = each.value.destination
    gateway           = each.value.gateway
    interface         = one([for tunnel in var.network[each.key].tunnels : tunnel.interface if tunnel.peer_inner_ipv6 == each.value.gateway])
    interval_seconds  = each.value.interval_seconds
    failure_threshold = each.value.failure_threshold
    route_metric      = each.value.route_metric
  })

  depends_on = [proxmox_virtual_environment_container.routing_simulator]
}

resource "pveguest_systemd_unit" "route_tracker" {
  for_each = var.route_trackers

  node = local.routing_guests[each.key].node
  vmid = local.routing_guests[each.key].vmid
  kind = local.routing_guest_kind

  name    = local.tracker_unit_name
  enabled = true
  active  = true

  restart_on = {
    script = pveguest_file.route_tracker_script[each.key].write_id
    unit   = pveguest_file.route_tracker_unit[each.key].write_id
  }

  depends_on = [
    pveguest_systemd_unit.networkd,
    pveguest_systemd_unit.frr,
  ]
}
