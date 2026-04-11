package quagga

import "goodkind.io/mwan/pkg/opnsense-go/api"

const quaggaReconfigureEndpoint = "/quagga/service/reconfigure"

// Controller for quagga
type Controller struct {
	Api *api.Client
}

func (c *Controller) Client() *api.Client {
	return c.Api
}
