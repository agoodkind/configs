locals {
  legacy_app_guest         = local.guests["tack_app2_suburban"]
  legacy_app_kafka_brokers = "[3d06:bad:b01:210:7ac::3]:9094"
}

resource "pveguest_file" "legacy_app_kafka_unit" {
  node = local.legacy_app_guest.node
  vmid = local.legacy_app_guest.vmid
  kind = local.legacy_app_guest.kind

  path = "/etc/systemd/system/tack-legacy-app-kafka.service"
  content = join("\n", [
    "[Unit]",
    "Description=Update the legacy Tack app Kafka endpoint",
    "After=docker.service",
    "AssertPathExists=/root/tack/.env",
    "",
    "[Service]",
    "Type=oneshot",
    "RemainAfterExit=yes",
    "WorkingDirectory=/root/tack",
    "TimeoutStartSec=280",
    "ExecStartPre=/usr/bin/grep -q ^AUDIT_KAFKA_BROKERS= .env",
    "ExecStart=/usr/bin/sed -i -E s/^AUDIT_KAFKA_BROKERS=.*/AUDIT_KAFKA_BROKERS=${local.legacy_app_kafka_brokers}/ .env",
    "ExecStart=/usr/bin/docker compose up -d --wait --wait-timeout 60 --no-deps --no-build app",
    "ExecStartPost=/usr/bin/curl --fail --silent --show-error --max-time 5 --retry 40 --retry-delay 5 --retry-all-errors --retry-max-time 200 http://[::1]:8000/healthz",
    "",
  ])
}

resource "pveguest_systemd_unit" "legacy_app_kafka" {
  node = local.legacy_app_guest.node
  vmid = local.legacy_app_guest.vmid
  kind = local.legacy_app_guest.kind

  name    = "tack-legacy-app-kafka.service"
  enabled = false
  active  = true

  restart_on = {
    unit     = pveguest_file.legacy_app_kafka_unit.write_id
    endpoint = local.legacy_app_kafka_brokers
  }

  depends_on = [pveguest_systemd_unit.k3s_kafka_listener]
}
