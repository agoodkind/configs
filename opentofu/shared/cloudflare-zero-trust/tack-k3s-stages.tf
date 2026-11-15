variable "tack_k3s_environments" {
  description = "Desired production connector environment."
  type        = set(string)
  default     = ["prod"]

  validation {
    condition     = length(var.tack_k3s_environments) > 0 && length(setsubtract(var.tack_k3s_environments, toset(["prod"]))) == 0
    error_message = "Select prod as the desired connector environment."
  }
}
