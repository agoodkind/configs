locals {
  wait_units = {
    for connector in keys(local.connectors) : connector => <<-UNIT
      [Unit]
      Description=Wait for ${var.connector_prefix}-${connector} desired revision and readiness
      Requires=k3s.service
      After=k3s.service

      [Service]
      Type=oneshot
      RemainAfterExit=yes
      TimeoutStartSec=280
      ExecStart=/usr/local/bin/k3s kubectl wait --namespace=${var.namespace} --for=create deployment/${var.connector_prefix}-${connector} --timeout=30s
      ExecStart=/usr/local/bin/k3s kubectl wait --namespace=${var.namespace} --for=jsonpath={.spec.template.metadata.labels.${var.revision_label}}=${local.revisions[connector]} deployment/${var.connector_prefix}-${connector} --timeout=60s
      ExecStart=/usr/local/bin/k3s kubectl rollout status --namespace=${var.namespace} deployment/${var.connector_prefix}-${connector} --timeout=180s
    UNIT
  }
}

resource "pveguest_file" "wait_unit" {
  for_each = local.wait_units
  node     = var.guest.node
  vmid     = var.guest.vmid
  kind     = "qemu"
  path     = "/etc/systemd/system/${var.manifest_prefix}-${each.key}-ready.service"
  content  = each.value
}

resource "pveguest_file" "connector_a" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/var/lib/rancher/k3s/server/manifests/${var.manifest_prefix}-a.yaml"
  content = yamlencode(local.deployments.a)

  depends_on = [pveguest_file.token]
}

resource "pveguest_systemd_unit" "connector_a_ready" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  name    = "${var.manifest_prefix}-a-ready.service"
  enabled = false
  active  = true
  restart_on = {
    manifest = pveguest_file.connector_a.write_id
    secret   = pveguest_file.token.write_id
    unit     = pveguest_file.wait_unit["a"].write_id
  }
}

resource "pveguest_file" "connector_b" {
  count   = var.connector_count == 2 ? 1 : 0
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/var/lib/rancher/k3s/server/manifests/${var.manifest_prefix}-b.yaml"
  content = yamlencode(local.deployments.b)

  depends_on = [pveguest_systemd_unit.connector_a_ready]
}

resource "pveguest_systemd_unit" "connector_b_ready" {
  count   = var.connector_count == 2 ? 1 : 0
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  name    = "${var.manifest_prefix}-b-ready.service"
  enabled = false
  active  = true
  restart_on = {
    manifest = pveguest_file.connector_b[0].write_id
    secret   = pveguest_file.token.write_id
    unit     = pveguest_file.wait_unit["b"].write_id
  }
}

moved {
  from = pveguest_file.connector_b
  to   = pveguest_file.connector_b[0]
}
moved {
  from = pveguest_systemd_unit.connector_b_ready
  to   = pveguest_systemd_unit.connector_b_ready[0]
}
