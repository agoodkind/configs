//go:build linux

package oob

import (
	"reflect"
	"testing"
)

func TestParseRuleList(t *testing.T) {
	in := `0:	from all lookup local
5:	from all uidrange 997-997 lookup oob
6:	from 3d06:bad:b01:ff::1 lookup oob
32766:	from all lookup main
`
	want := []CurrentRule{
		{Priority: 0, From: "all", Table: "local"},
		{Priority: 5, From: "all", UIDRange: "997-997", Table: "oob"},
		{Priority: 6, From: "3d06:bad:b01:ff::1", Table: "oob"},
		{Priority: 32766, From: "all", Table: "main"},
	}
	got, err := parseRuleList(in)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestRulesMatch(t *testing.T) {
	cur := CurrentRule{Priority: 5, From: "all", UIDRange: "997-997", Table: "oob"}
	cases := []struct {
		name string
		want DesiredRule
		ok   bool
	}{
		{"exact", DesiredRule{Priority: 5, From: "all", UIDRange: "997-997", Table: "oob"}, true},
		{"diff prio", DesiredRule{Priority: 6, From: "all", UIDRange: "997-997", Table: "oob"}, false},
		{"diff uid", DesiredRule{Priority: 5, From: "all", UIDRange: "996-996", Table: "oob"}, false},
		{"diff table", DesiredRule{Priority: 5, From: "all", UIDRange: "997-997", Table: "main"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rulesMatch(cur, tc.want) != tc.ok {
				t.Fatalf("rulesMatch(%+v, %+v) = %v, want %v",
					cur, tc.want, !tc.ok, tc.ok)
			}
		})
	}
}
