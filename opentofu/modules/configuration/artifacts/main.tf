output "debian_cloud_image" {
  value = {
    url       = "https://cloud.debian.org/images/cloud/trixie/20261103-2651/debian-13-genericcloud-amd64-20261103-2651.qcow2"
    file_name = "debian-13-genericcloud-amd64-20261103-2651.qcow2"
    sha512    = "d25999de0770d4cde1be72c299ba3ad94eac7ea8fd4d18f8cc86c69df69337e2e8a91d64465a9bfd8f2a78c566fcbcb0842eacc91c09efdef72d719d9f13b535"
  }
}

output "k3s" {
  value = {
    version = "v1.37.2+k3s1"
    url     = "https://github.com/k3s-io/k3s/releases/download/v1.37.2%2Bk3s1/k3s"
    sha256  = "265b3ee6a3198ef14033c63ef46cbb32370d83795199fb6d1b01f21575eddbda"
  }
}

output "tack_store_images" {
  value = {
    foundationdb = "foundationdb/foundationdb:7.4.6@sha256:1db13771714a1b2cc565c893e2ba768b5a3c33d101c84f2f6fda32f075cc8763"
    yugabyte     = "yugabytedb/yugabyte:2024.2.8.0-b85@sha256:1a5b4be65d74563db7b2625fbbb302b4218539f4d729e35b8a4c78048e37a2e6"
    kafka        = "apache/kafka:4.2.0@sha256:9516fb7634bad307d17c33b589fde9023003b0cb761374f500002b980a3149b9"
    opensearch   = "opensearchproject/opensearch:3.8.0@sha256:fafe3fc3587088674669235575aa166228c48bdb940294a8cdbbc1da75236a40"
  }
}

output "tack_overlays" {
  value = {
    "fdb-overlay/fdb.bash"       = "3b53cee50929e7d94fa24e97d3b39ae955c2840684a3b07ba0d31e4e228b98e8"
    "yugabyte-overlay/yugabyted" = "5f6cba4648db9f644a9335a853b9fd1726c615c4d5f722e1425419cb72238f5f"
  }
}
