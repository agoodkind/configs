locals {
  node_name = "worker-${var.guest.vmid}-${lower(var.build_uuid)}"
  enrollment = {
    HYPERVISOR = var.hypervisor
    SERVER     = tostring(var.server_vmid)
    WORKER     = tostring(var.guest.vmid)
    BUILD_UUID = lower(var.build_uuid)
    SCRIPT     = "${path.module}/files/enroll-worker.sh"
  }
}

resource "terraform_data" "enrollment" {
  input            = local.enrollment
  triggers_replace = [var.build_uuid, var.hypervisor, var.server_vmid, var.guest.vmid]

  provisioner "local-exec" {
    environment = local.enrollment
    command     = <<-SH
      ssh -o BatchMode=yes -- "root@$HYPERVISOR" \
        bash -s -- enroll "$SERVER" "$WORKER" "$BUILD_UUID" < "$SCRIPT"
    SH
  }

  provisioner "local-exec" {
    when        = destroy
    environment = self.input
    command     = <<-SH
      ssh -o BatchMode=yes -- "root@$HYPERVISOR" \
        bash -s -- remove "$SERVER" "$WORKER" "$BUILD_UUID" < "$SCRIPT"
    SH
  }

  depends_on = [
    module.runtime,
    pveguest_file.configuration,
    pveguest_file.start,
    pveguest_file.service,
  ]
}
