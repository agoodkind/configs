variable "tack_k3s_environments" {
  description = "Desired connector environments. Initial migration applies QA before adding production."
  type        = set(string)
  default     = ["qa", "prod"]

  validation {
    condition     = length(var.tack_k3s_environments) > 0 && length(setsubtract(var.tack_k3s_environments, toset(["qa", "prod"]))) == 0
    error_message = "Select qa, prod, or both as the desired connector environments."
  }
}
