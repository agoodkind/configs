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

func TestInjectGatewayV6_AddsWhenMissing(t *testing.T) {
	input := []byte(`<opnsense>
  <interfaces>
    <wan>
      <ipaddr>10.250.250.2</ipaddr>
      <gateway>WAN_GW4</gateway>
      <subnet>29</subnet>
    </wan>
  </interfaces>
</opnsense>`)

	out, changed := injectGatewayV6(input, "WAN_GW6")
	if !changed {
		t.Fatal("expected changed=true when gatewayv6 absent")
	}
	if !strings.Contains(string(out), "<gatewayv6>WAN_GW6</gatewayv6>") {
		t.Fatalf("injected gatewayv6 not in output:\n%s", out)
	}
	// Must still be inside <wan>
	if !strings.Contains(string(out), "</wan>") ||
		!strings.Contains(string(out), "<gatewayv6>WAN_GW6</gatewayv6>") {
		t.Fatalf("structure broken:\n%s", out)
	}
	if !strings.Contains(string(out), "gateway>WAN_GW4") {
		t.Fatalf("v4 gateway accidentally removed:\n%s", out)
	}
	// Non-destructive on other content
	if !strings.Contains(string(out), "subnet>29") {
		t.Fatalf("subnet was accidentally removed:\n%s", out)
	}
}

func TestInjectGatewayV6_NoOpWhenPresent(t *testing.T) {
	input := []byte(`<opnsense>
  <interfaces>
    <wan>
      <ipaddr>10.250.250.2</ipaddr>
      <gatewayv6>WAN_GW6</gatewayv6>
    </wan>
  </interfaces>
</opnsense>`)

	_, changed := injectGatewayV6(input, "WAN_GW6")
	if changed {
		t.Fatal("expected no change when gatewayv6 already present")
	}
}

func TestInjectGatewayV6_DoesNotTouchOtherSections(t *testing.T) {
	input := []byte(`<opnsense>
  <interfaces>
    <lan>
      <ipaddr>192.168.1.1</ipaddr>
    </lan>
    <wan>
      <ipaddr>10.250.250.2</ipaddr>
    </wan>
  </interfaces>
</opnsense>`)

	out, changed := injectGatewayV6(input, "WAN_GW6")
	if !changed {
		t.Fatal("expected changed")
	}
	// Should NOT add to <lan>
	lanIdx := strings.Index(string(out), "<lan>")
	lanCloseIdx := strings.Index(string(out), "</lan>")
	if strings.Contains(string(out)[lanIdx:lanCloseIdx], "gatewayv6") {
		t.Fatalf("gatewayv6 leaked into <lan>:\n%s", out)
	}
	// Must be inside <wan>
	wanIdx := strings.Index(string(out), "<wan>")
	wanCloseIdx := strings.Index(string(out), "</wan>")
	if !strings.Contains(string(out)[wanIdx:wanCloseIdx], "<gatewayv6>WAN_GW6</gatewayv6>") {
		t.Fatalf("gatewayv6 not inside <wan>:\n%s", out)
	}
}

func TestInjectGatewayV6_RoundtripWithStrip(t *testing.T) {
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

	stripped, _ := stripGatewayV6(input)
	restored, changed := injectGatewayV6(stripped, "WAN_GW6")
	if !changed {
		t.Fatal("expected restored to differ from stripped")
	}
	// Restored must contain gatewayv6 again
	if !strings.Contains(string(restored), "<gatewayv6>WAN_GW6</gatewayv6>") {
		t.Fatalf("roundtrip lost gatewayv6:\n%s", restored)
	}
	// Original other tags preserved
	for _, want := range []string{"<gateway>WAN_GW4</gateway>", "<subnet>29</subnet>", "<ipaddr>10.250.250.2</ipaddr>"} {
		if !strings.Contains(string(restored), want) {
			t.Errorf("missing %q after roundtrip:\n%s", want, restored)
		}
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
