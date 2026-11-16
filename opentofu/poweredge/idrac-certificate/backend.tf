terraform {
  backend "local" {
    path = "/var/lib/configs-idrac-certificate/terraform.tfstate"
  }
}
