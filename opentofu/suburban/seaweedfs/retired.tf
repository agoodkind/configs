# Forget records on the deleted guest without contacting that guest.
removed {
  from = module.base
  lifecycle { destroy = false }
}

removed {
  from = pveguest_file.seaweedfs_compaction_limit
  lifecycle { destroy = false }
}

removed {
  from = pveguest_systemd_unit.seaweedfs_compaction_limit
  lifecycle { destroy = false }
}
