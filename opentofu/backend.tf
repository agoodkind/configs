terraform {
  backend "consul" {
    address = "[3d06:bad:b01::106]:8500"
    path    = "opentofu/state"
    scheme  = "http"
  }
}
