// Package opnsensesvc carries the channel-split partition
// definitions here: the short/long method lists below pin the
// per-port allowlist used by the MWAN-184 Fix 4 dispatcher split.
package opnsensesvc

import (
	"goodkind.io/mwan/internal/mwn1"
)

// Channel split (MWAN-184 Fix 4) partitions the MWANOPNsense RPC
// surface across two virtio-serial ports so that a wedge on a
// long-running RPC port cannot starve the short-RPC port. Reset is
// implicitly allowed on every port regardless of these lists.

// ShortChannelMethods are the unary RPCs that the operator expects
// to complete in milliseconds. These run on the original
// `<vmid>.mwanrpc` socket. A wedge on this port is the operational
// disaster that the watchdog and Reset RPC together aim to recover
// from.
var ShortChannelMethods = []uint16{
	mwn1.MethodVersion,
	mwn1.MethodReadConfigXML,
	mwn1.MethodWriteConfigXML,
	mwn1.MethodBackupConfigXML,
	mwn1.MethodXPathGet,
	mwn1.MethodXPathSet,
	mwn1.MethodXPathDelete,
	mwn1.MethodStripGatewayV6,
	mwn1.MethodInjectGatewayV6,
	mwn1.MethodDeployStatus,
}

// LongChannelMethods are the RPCs that can hold a worker for seconds
// to minutes. These run on the optional `<vmid>.mwanrpc-long` socket
// when channel split is enabled.
var LongChannelMethods = []uint16{
	mwn1.MethodExec,
	mwn1.MethodDeploy,
	mwn1.MethodRevert,
}
