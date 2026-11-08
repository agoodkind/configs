output "server_container_id" {
  description = "Docker container ID of the FoundationDB server."
  value       = docker_container.server.id
}

output "backup_agent_container_id" {
  description = "Docker container ID of the FoundationDB backup agent."
  value       = docker_container.backup_agent.id
}
