package mwn1

import (
	"google.golang.org/protobuf/proto"

	mwanv1 "goodkind.io/mwan/gen/mwan/v1"
)

// Wire method ids for the MWANOPNsenseService RPCs. Assigned in the
// order RPCs are declared in mwan/v1/mwan_opnsense.proto. Once shipped,
// these ids are immutable; new RPCs take the next free id.
const (
	MethodVersion         uint16 = 1
	MethodExec            uint16 = 2
	MethodReadConfigXML   uint16 = 3
	MethodWriteConfigXML  uint16 = 4
	MethodBackupConfigXML uint16 = 5
	MethodXPathGet        uint16 = 6
	MethodXPathSet        uint16 = 7
	MethodXPathDelete     uint16 = 8
	MethodStripGatewayV6  uint16 = 9
	MethodInjectGatewayV6 uint16 = 10
	MethodDeploy          uint16 = 11
	MethodDeployStatus    uint16 = 12
	MethodRevert          uint16 = 13
	MethodReset           uint16 = 14
)

// MWANOPNsenseServicePrefix is the proto-fully-qualified service name
// used as the prefix for method names registered in the registry.
const MWANOPNsenseServicePrefix = "mwan.v1.MWANOPNsenseService/"

// mwanOpnsenseEntry describes one row of the static registration table.
type mwanOpnsenseEntry struct {
	id          uint16
	method      string
	newRequest  MessageFactory
	newResponse MessageFactory
}

// mwanOpnsenseEntries lists every RPC in declaration order. Deploy is
// a client-streaming RPC whose request type is Chunk; the registry
// records Chunk as the per-frame request factory. The DeployResponse
// is the single terminal response.
var mwanOpnsenseEntries = []mwanOpnsenseEntry{
	{
		MethodVersion, "Version",
		func() proto.Message { return &mwanv1.VersionRequest{} },
		func() proto.Message { return &mwanv1.VersionResponse{} },
	},
	{
		MethodExec, "Exec",
		func() proto.Message { return &mwanv1.ExecRequest{} },
		func() proto.Message { return &mwanv1.ExecResponse{} },
	},
	{
		MethodReadConfigXML, "ReadConfigXML",
		func() proto.Message { return &mwanv1.ReadConfigXMLRequest{} },
		func() proto.Message { return &mwanv1.ReadConfigXMLResponse{} },
	},
	{
		MethodWriteConfigXML, "WriteConfigXML",
		func() proto.Message { return &mwanv1.WriteConfigXMLRequest{} },
		func() proto.Message { return &mwanv1.WriteConfigXMLResponse{} },
	},
	{
		MethodBackupConfigXML, "BackupConfigXML",
		func() proto.Message { return &mwanv1.BackupConfigXMLRequest{} },
		func() proto.Message { return &mwanv1.BackupConfigXMLResponse{} },
	},
	{
		MethodXPathGet, "XPathGet",
		func() proto.Message { return &mwanv1.XPathGetRequest{} },
		func() proto.Message { return &mwanv1.XPathGetResponse{} },
	},
	{
		MethodXPathSet, "XPathSet",
		func() proto.Message { return &mwanv1.XPathSetRequest{} },
		func() proto.Message { return &mwanv1.XPathSetResponse{} },
	},
	{
		MethodXPathDelete, "XPathDelete",
		func() proto.Message { return &mwanv1.XPathDeleteRequest{} },
		func() proto.Message { return &mwanv1.XPathDeleteResponse{} },
	},
	{
		MethodStripGatewayV6, "StripGatewayV6",
		func() proto.Message { return &mwanv1.StripGatewayV6Request{} },
		func() proto.Message { return &mwanv1.StripGatewayV6Response{} },
	},
	{
		MethodInjectGatewayV6, "InjectGatewayV6",
		func() proto.Message { return &mwanv1.InjectGatewayV6Request{} },
		func() proto.Message { return &mwanv1.InjectGatewayV6Response{} },
	},
	{
		MethodDeploy, "Deploy",
		func() proto.Message { return &mwanv1.Chunk{} },
		func() proto.Message { return &mwanv1.DeployResponse{} },
	},
	{
		MethodDeployStatus, "DeployStatus",
		func() proto.Message { return &mwanv1.DeployStatusRequest{} },
		func() proto.Message { return &mwanv1.DeployStatusResponse{} },
	},
	{
		MethodRevert, "Revert",
		func() proto.Message { return &mwanv1.RevertRequest{} },
		func() proto.Message { return &mwanv1.RevertResponse{} },
	},
	{
		MethodReset, "Reset",
		func() proto.Message { return &mwanv1.ResetRequest{} },
		func() proto.Message { return &mwanv1.ResetResponse{} },
	},
}

// RegisterMWANOPNsenseService registers every MWANOPNsenseService RPC
// against reg. It returns the first registration error, if any.
func RegisterMWANOPNsenseService(reg *Registry) error {
	for _, entry := range mwanOpnsenseEntries {
		if err := reg.Register(entry.id, MWANOPNsenseServicePrefix+entry.method,
			entry.newRequest, entry.newResponse); err != nil {
			return err
		}
	}
	return nil
}

// NewMWANOPNsenseRegistry constructs a Registry preloaded with every
// MWANOPNsenseService method. The static mwanOpnsenseEntries slice is
// covered by TestRegistry_MWANOPNsenseAssignments which guarantees no
// duplicate id or name slips into production; a registration error
// here can only mean the table itself was edited incorrectly, in
// which case the registry is returned with whatever entries did
// register and the error tells the caller which one failed.
//
// Production callers should treat a non-nil error as a startup-fatal
// configuration bug.
func NewMWANOPNsenseRegistry() (*Registry, error) {
	reg := NewRegistry()
	if err := RegisterMWANOPNsenseService(reg); err != nil {
		return reg, err
	}
	return reg, nil
}
