package cutover2

import "testing"

func TestPickV6GatewayName_ByName(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"WAN_GW4", "WAN_GW6"}, "WAN_GW6"},
		{[]string{"WAN_GW6", "WAN_GW4"}, "WAN_GW6"},
		{[]string{"PRIMARY_V4", "PRIMARY_V6"}, "PRIMARY_V6"},
		{[]string{"GW4", "GW6"}, "GW6"},
		// fallback path: no name match -> second entry
		{[]string{"GW_FIRST", "GW_SECOND"}, "GW_SECOND"},
		// empty
		{nil, ""},
		// single entry, no v6 marker -> empty (no second entry)
		{[]string{"WAN_GW4"}, ""},
	}
	for _, c := range cases {
		got := pickV6GatewayName(c.in)
		if got != c.want {
			t.Errorf("pickV6GatewayName(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
