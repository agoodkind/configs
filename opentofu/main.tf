locals {
  shared_vars = yamldecode(
    file("${path.module}/../ansible/inventory/group_vars/all/vars.yml")
  )
}
