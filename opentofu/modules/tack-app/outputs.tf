output "container_id" {
  description = "ID of the app container. OpenTofu assigns a new ID when it replaces the container."
  value       = docker_container.app.id
}

output "container_name" {
  description = "Name of the app container."
  value       = docker_container.app.name
}
