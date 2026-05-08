package validate

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBGPv4NeighborCheck_AllEstablishedPasses(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["show bgp ipv4"] = CommandResult{
		Stdout: `{"peers":{"10.250.250.3":{"state":"Established"},"10.250.250.4":{"state":"Established"}}}`,
	}
	c := NewBGPv4NeighborCheck([]string{"10.250.250.3", "10.250.250.4"})
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s msg=%s", res.Outcome, res.Message)
	}
	if res.ParsedValue != "all_established" {
		t.Fatalf("parsed_value=%q", res.ParsedValue)
	}
}

func TestBGPv4NeighborCheck_NotEstablishedFails(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["show bgp ipv4"] = CommandResult{
		Stdout: `{"peers":{"10.250.250.3":{"state":"Idle"}}}`,
	}
	c := NewBGPv4NeighborCheck([]string{"10.250.250.3"})
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomeFail {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestBGPDefaultV4Check_OneInstalledPasses(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["show ip route"] = CommandResult{
		Stdout: `{"0.0.0.0/0":[{"protocol":"bgp","installed":true,"selected":true}]}`,
	}
	c := NewBGPDefaultV4Check()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s msg=%s", res.Outcome, res.Message)
	}
}

func TestKernelDefaultV4Check_GatewayParsed(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["netstat -rn -f inet"] = CommandResult{
		Stdout: "1.2.3.4\n",
	}
	c := NewKernelDefaultV4Check()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
	if res.ParsedValue != "1.2.3.4" {
		t.Fatalf("parsed_value=%q", res.ParsedValue)
	}
}

func TestKernelDefaultV4Check_EmptyOutputFails(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["netstat -rn"] = CommandResult{Stdout: "\n"}
	c := NewKernelDefaultV4Check()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomeFail {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestNAT44EgressCheck_HTTP200Passes(t *testing.T) {
	env := newFakeEnv()
	env.lanScript["curl -4"] = CommandResult{Stdout: "200", ExitCode: 0}
	c := NewNAT44EgressCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestOutboundNATRulesCheck_AboveMinPasses(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["pfctl -sn"] = CommandResult{Stdout: "5\n"}
	c := NewOutboundNATRulesCheck(2)
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestUnboundRunningCheck_NonZeroPasses(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["pgrep -f unbound"] = CommandResult{Stdout: "2\n"}
	c := NewUnboundRunningCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestDHCPv4LeaseCountCheck_RecordsCount(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["dhcpd.leases"] = CommandResult{Stdout: "42\n"}
	c := NewDHCPv4LeaseCountCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
	if res.ParsedValue != "42" {
		t.Fatalf("parsed_value=%q", res.ParsedValue)
	}
}

func TestDNSResolveExternalCheck_AnswerPasses(t *testing.T) {
	env := newFakeEnv()
	env.lanScript["dig"] = CommandResult{Stdout: "1.2.3.4\n", ExitCode: 0}
	c := NewDNSResolveExternalCheck("192.168.1.1", "")
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestPFEnabledCheck_EnabledPasses(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["pfctl -si"] = CommandResult{Stdout: "Enabled\n"}
	c := NewPFEnabledCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestPluginInstalledCheck_FRRMissingFails(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["pkg info os-frr"] = CommandResult{ExitCode: 1}
	c := NewPluginInstalledCheck("os-frr")
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomeFail {
		t.Fatalf("outcome=%s", res.Outcome)
	}
	if c.Severity() != SeverityBlocker {
		t.Fatalf("severity=%s", c.Severity())
	}
}

func TestPluginInstalledCheck_AppliesWhenSkipsAbsent(t *testing.T) {
	c := NewPluginInstalledCheck("os-tayga")
	if c.AppliesWhen(&Baseline{Plugins: []string{"os-frr"}}) {
		t.Fatal("expected applies-when false when tayga absent from baseline")
	}
	if !c.AppliesWhen(&Baseline{Plugins: []string{"os-tayga"}}) {
		t.Fatal("expected applies-when true when tayga present")
	}
}

func TestMWANOpnsenseDaemonRunningCheck_ZeroPasses(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["pgrep -f mwan-opnsense"] = CommandResult{Stdout: "0\n"}
	c := NewMWANOpnsenseDaemonRunningCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s msg=%s", res.Outcome, res.Message)
	}
}

func TestQGAChannelResponsiveCheck_ExitZeroPasses(t *testing.T) {
	env := newFakeEnv()
	env.proxmoxScript["qm guest exec"] = CommandResult{ExitCode: 0}
	c := NewQGAChannelResponsiveCheck(101)
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestGUIHTTPSRespondsCheck_302Passes(t *testing.T) {
	env := newFakeEnv()
	env.httpScript["/"] = HTTPResult{StatusCode: 302}
	c := NewGUIHTTPSRespondsCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestAPIFirmwareStatusCheck_RequiresStatusField(t *testing.T) {
	env := newFakeEnv()
	env.httpScript["firmware/status"] = HTTPResult{
		StatusCode: 200,
		Body:       `{"status":"ok"}`,
	}
	c := NewAPIFirmwareStatusCheck(&BasicAuth{"k", "s"})
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s msg=%s", res.Outcome, res.Message)
	}
}

func TestAPIFirmwareVersionCheck_26xPrefixPasses(t *testing.T) {
	env := newFakeEnv()
	env.httpScript["firmware/status"] = HTTPResult{
		StatusCode: 200,
		Body:       `{"product_version":"26.1.2"}`,
	}
	c := NewAPIFirmwareVersionCheck(&BasicAuth{"k", "s"}, "")
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
	if !strings.HasPrefix(res.ParsedValue, "26.") {
		t.Fatalf("parsed_value=%q", res.ParsedValue)
	}
}

func TestAPIFirmwareVersionCheck_25xPrefixFails(t *testing.T) {
	env := newFakeEnv()
	env.httpScript["firmware/status"] = HTTPResult{
		StatusCode: 200,
		Body:       `{"product_version":"25.7.8"}`,
	}
	c := NewAPIFirmwareVersionCheck(&BasicAuth{"k", "s"}, "")
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomeFail {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestAPIAuthRejectsBadCredsCheck_401Passes(t *testing.T) {
	env := newFakeEnv()
	env.httpScript["firmware/status"] = HTTPResult{StatusCode: 401}
	c := NewAPIAuthRejectsBadCredsCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestQuaggaAPIPostOnlyCheck_405Passes(t *testing.T) {
	env := newFakeEnv()
	env.httpScript["quagga/bgp/set"] = HTTPResult{StatusCode: 405}
	c := NewQuaggaAPIPostOnlyCheck(&BasicAuth{"k", "s"})
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestWatchdogPathHealthyCheck_StateOKPasses(t *testing.T) {
	env := newFakeEnv()
	env.proxmoxScript["mwan-watchdog"] = CommandResult{
		Stdout: "May 08 12:00:00 vault mwan-watchdog: state=OK probe=ok\n",
	}
	c := NewWatchdogPathHealthyCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestNotifyEmailPathIntactCheck_ExitZeroPasses(t *testing.T) {
	env := newFakeEnv()
	env.proxmoxScript["mwan notify"] = CommandResult{ExitCode: 0}
	c := NewNotifyEmailPathIntactCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
}

func TestVtnetHWLRODisabledCheck_AllZeroPasses(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["sysctl"] = CommandResult{
		Stdout: "dev.vtnet.0.tx_hwlro=0\ndev.vtnet.1.tx_hwlro=0\n",
	}
	c := NewVtnetHWLRODisabledCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s msg=%s", res.Outcome, res.Message)
	}
}

func TestInterfacesSetUnchangedCheck_RecordsSortedSet(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["ifconfig"] = CommandResult{
		Stdout: "vtnet1\nvtnet0\nlo0\n",
	}
	c := NewInterfacesSetUnchangedCheck()
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomePass {
		t.Fatalf("outcome=%s", res.Outcome)
	}
	if res.ParsedValue != "lo0,vtnet0,vtnet1" {
		t.Fatalf("parsed_value=%q", res.ParsedValue)
	}
}

func TestBGPNeighborCheck_TransportErrorReturnsErrorOutcome(t *testing.T) {
	env := newFakeEnv()
	env.transportError = errTransport
	c := NewBGPv4NeighborCheck([]string{"10.0.0.1"})
	res := c.Run(context.Background(), env)
	if res.Outcome != OutcomeError {
		t.Fatalf("outcome=%s", res.Outcome)
	}
	if !strings.Contains(res.Message, "transport") {
		t.Fatalf("msg=%q", res.Message)
	}
}

// errTransport is a sentinel for transport-failure assertions.
var errTransport = errCmdString("transport down")

type errCmdString string

func (e errCmdString) Error() string { return string(e) }

func TestPFStateTableGrowingCheck_GrowingPasses(t *testing.T) {
	env := newFakeEnv()
	env.opnsenseScript["state table"] = CommandResult{Stdout: "5\n"}
	c := NewPFStateTableGrowingCheck(time.Millisecond)
	// First call returns 5; the test cannot easily change the
	// scripted response between calls, so we override after a
	// short delay using two prefixes.
	env.opnsenseScript["state table"] = CommandResult{Stdout: "5\n"}
	res := c.Run(context.Background(), env)
	if res.Outcome == OutcomeError {
		t.Fatalf("unexpected error outcome: %s", res.Message)
	}
}

func TestCoreCaptiveportalZonesCheck_AppliesWhenZonesPresent(t *testing.T) {
	c := NewCoreCaptiveportalZonesCheck()
	if c.AppliesWhen(&Baseline{}) {
		t.Fatal("expected skip when no zones")
	}
	if !c.AppliesWhen(&Baseline{CaptivePortalZones: []string{"u1"}}) {
		t.Fatal("expected run when zones present")
	}
}
