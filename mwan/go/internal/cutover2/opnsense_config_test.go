package cutover2

import (
	"strings"
	"testing"
)

func TestStripGatewayV6_RemovesElement(t *testing.T) {
	input := []byte(`<opnsense>
  <interfaces>
    <wan>
      <ipaddr>10.250.250.2</ipaddr>
      <gateway>WAN_GW4</gateway>
      <gatewayv6>WAN_GW6</gatewayv6>
      <subnet>29</subnet>
    </wan>
  </interfaces>
</opnsense>`)

	out, changed := stripGatewayV6(input)
	if !changed {
		t.Fatal("expected changed=true")
	}
	if strings.Contains(string(out), "gatewayv6") {
		t.Fatalf("gatewayv6 still present in output:\n%s", out)
	}
	if !strings.Contains(string(out), "gateway>WAN_GW4") {
		t.Fatalf("gateway (v4) was accidentally removed:\n%s", out)
	}
	if !strings.Contains(string(out), "subnet>29") {
		t.Fatalf("subnet was accidentally removed:\n%s", out)
	}
}

func TestStripGatewayV6_NoChange(t *testing.T) {
	input := []byte(`<opnsense>
  <interfaces>
    <wan>
      <ipaddr>10.250.250.2</ipaddr>
      <gateway>WAN_GW4</gateway>
      <subnet>29</subnet>
    </wan>
  </interfaces>
</opnsense>`)

	_, changed := stripGatewayV6(input)
	if changed {
		t.Fatal("expected changed=false when gatewayv6 is absent")
	}
}

func TestStripGatewayV6_OnlyRemovesFromWan(t *testing.T) {
	input := []byte(`<opnsense>
  <interfaces>
    <lan>
      <gatewayv6>LAN_GW6</gatewayv6>
    </lan>
    <wan>
      <gatewayv6>WAN_GW6</gatewayv6>
    </wan>
  </interfaces>
</opnsense>`)

	out, changed := stripGatewayV6(input)
	if !changed {
		t.Fatal("expected changed=true")
	}
	outStr := string(out)
	if strings.Contains(outStr, "WAN_GW6") {
		t.Fatalf("WAN gatewayv6 still present:\n%s", outStr)
	}
	if !strings.Contains(outStr, "LAN_GW6") {
		t.Fatalf("LAN gatewayv6 was accidentally removed:\n%s", outStr)
	}
}
