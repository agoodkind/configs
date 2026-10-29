variable "response_body" { type = string }

output "public_keys" {
  value = [for line in split("\n", replace(var.response_body, "\r", "")) : trimspace(line) if trimspace(line) != ""]
}
