module "seaweedfs_suburban" { source = "../../services/seaweedfs_suburban" }

locals {
  tack_fdb_image = {
    version = "7.4.6"
    id      = "sha256:1db13771714a1b2cc565c893e2ba768b5a3c33d101c84f2f6fda32f075cc8763"
  }

  tack_fdb_overlay = {
    path   = "/root/tack/fdb-overlay/fdb.bash"
    sha256 = "3b53cee50929e7d94fa24e97d3b39ae955c2840684a3b07ba0d31e4e228b98e8"
  }

  tack_fdb_state_volume_labels = {
    "com.docker.compose.config-hash" = "06ae176e9a7db15ed7b03a744d11d53c4d6762705741f9b81b9c9c9e4762caf8"
    "com.docker.compose.project"     = "tack"
    "com.docker.compose.version"     = "5.5.1"
    "com.docker.compose.volume"      = "fdb-data"
  }

  # Docker assigns this empty label to anonymous volumes. Match it when importing the volumes.
  tack_fdb_anonymous_volume_labels = { "com.docker.volume.anonymous" = "" }

  tack_fdb_members = {
    tack_data1_suburban = {
      volumes = {
        state      = { name = "tack_fdb-data", labels = local.tack_fdb_state_volume_labels }
        data       = { name = "5d3cdd57fba369a47d73695c07749100cfb02f3392d7c36c195fe7d0beae0372", labels = local.tack_fdb_anonymous_volume_labels }
        agent_data = { name = "e3f1f4cbe4ffc660ed60547a101a4ec5c7b33af4c67c41061db091fb8eda7c71", labels = local.tack_fdb_anonymous_volume_labels }
      }
    }
  }

  tack_fdb = {
    for key, member in local.tack_fdb_members : key => {
      image             = local.tack_fdb_image
      overlay           = local.tack_fdb_overlay
      public_address    = local.service_mapping[key].ipv6
      locality_id       = local.service_mapping[key].hostname
      blobstore_address = module.seaweedfs_suburban.value.ipv6
      volumes           = member.volumes
      network = {
        name         = "tack_default"
        ipv6_gateway = local.service_mapping[key].docker_v6_gateway
      }
    }
  }
}

output "tack_fdb" { value = local.tack_fdb }
