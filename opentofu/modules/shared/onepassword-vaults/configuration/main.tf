output "service_mapping" {
  value = {}
}

output "lock_targets" {
  value = {
    lock_free_types = ["opadmin_vault"]
  }
}

output "managed_guests" {
  value = {}
}
