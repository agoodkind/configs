locals {
  wait_units = {
    for connector in keys(local.connectors) : connector => <<-UNIT
      [Unit]
      Description=Wait for cloudflared-${connector} desired revision and readiness
      Requires=k3s.service
      After=k3s.service

      [Service]
      Type=oneshot
      RemainAfterExit=yes
      TimeoutStartSec=280
      ExecStart=/usr/local/bin/k3s kubectl wait --namespace=tack --for=create deployment/cloudflared-${connector} --timeout=30s
      ExecStart=/usr/local/bin/k3s kubectl wait --namespace=tack --for=jsonpath={.spec.template.metadata.labels.tack-revision}=${local.revisions[connector]} deployment/cloudflared-${connector} --timeout=60s
      ExecStart=/usr/local/bin/k3s kubectl rollout status --namespace=tack deployment/cloudflared-${connector} --timeout=180s
    UNIT
  }
}

resource "pveguest_file" "wait_unit" {
  for_each = local.wait_units
  node     = var.guest.node
  vmid     = var.guest.vmid
  kind     = "qemu"
  path     = "/etc/systemd/system/tack-cloudflared-${each.key}-ready.service"
  content  = each.value
}

resource "pveguest_file" "connector_a" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/var/lib/rancher/k3s/server/manifests/tack-cloudflared-a.yaml"
  content = yamlencode(local.deployments.a)

  depends_on = [pveguest_file.token]
}

resource "pveguest_systemd_unit" "connector_a_ready" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  name    = "tack-cloudflared-a-ready.service"
  enabled = false
  active  = true
  restart_on = {
    manifest = pveguest_file.connector_a.write_id
    secret   = pveguest_file.token.write_id
    unit     = pveguest_file.wait_unit["a"].write_id
  }
}

resource "pveguest_file" "connector_b" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  path    = "/var/lib/rancher/k3s/server/manifests/tack-cloudflared-b.yaml"
  content = yamlencode(local.deployments.b)

  depends_on = [pveguest_systemd_unit.connector_a_ready]
}

resource "pveguest_systemd_unit" "connector_b_ready" {
  node    = var.guest.node
  vmid    = var.guest.vmid
  kind    = "qemu"
  name    = "tack-cloudflared-b-ready.service"
  enabled = false
  active  = true
  restart_on = {
    manifest = pveguest_file.connector_b.write_id
    secret   = pveguest_file.token.write_id
    unit     = pveguest_file.wait_unit["b"].write_id
  }
}
