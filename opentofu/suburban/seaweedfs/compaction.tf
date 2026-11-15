resource "pveguest_file" "seaweedfs_compaction_limit" {
  node = local.guests.seaweedfs_suburban.node
  vmid = local.guests.seaweedfs_suburban.vmid
  kind = local.guests.seaweedfs_suburban.kind

  path    = "/etc/systemd/system/seaweedfs.service.d/compaction-limit.conf"
  content = <<-UNIT
    [Service]
    ExecStart=
    ExecStart=/usr/local/bin/weed server -dir=/var/lib/seaweedfs -s3 -s3.config=/etc/seaweedfs/s3.json -ip.bind=:: -master.volumeSizeLimitMB=1024 -volume.max=0 -volume.compactionMBps=8
  UNIT
}

resource "pveguest_systemd_unit" "seaweedfs_compaction_limit" {
  node = local.guests.seaweedfs_suburban.node
  vmid = local.guests.seaweedfs_suburban.vmid
  kind = local.guests.seaweedfs_suburban.kind

  name    = "seaweedfs.service"
  enabled = true
  active  = true

  restart_on = {
    compaction_limit = pveguest_file.seaweedfs_compaction_limit.write_id
  }
}
