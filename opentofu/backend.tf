terraform {
  # Local state on the operator workstation. The state file sits next to this
  # configuration and is gitignored; treat it as the single live copy.
  backend "local" {
    path = "terraform.tfstate"
  }
}
