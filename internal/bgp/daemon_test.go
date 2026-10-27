package bgp

import (
	"reflect"
	"testing"
)

func TestParseStatusOutput(t *testing.T) {
	cases := map[string]string{
		// gobgp neighbor <addr> --json
		`[{"state":{"neighbor-address":"192.0.2.2","peer-as":64500,"session-state":"established"},"timers":{"state":{"uptime":{"seconds":1}}}}]`: "established",
		`{"state":{"session-state":"active"}}`: "active",
		// FRR: show bgp neighbor <addr> json
		`{"192.0.2.2":{"remoteAs":64500,"bgpState":"Established","bgpTimerUpMsec":5000}}`: "established",
		// BIRD 2: show protocols all rtbh
		"rtbh       BGP        ---        up     2026-10-27  Established\n  BGP state:          Established\n    Neighbor address: 192.0.2.2": "established",
		"rtbh       BGP        ---        start  2026-10-27  Active        Socket: Connection refused\n  BGP state:          Active":          "active",
		"Idle":  "idle",
		"":      "unknown",
		"{}":    "unknown",
		"bogus": "unknown",
	}
	for in, want := range cases {
		if got := ParseStatusOutput(in); got.State != want {
			t.Errorf("ParseStatusOutput(%q).State = %q, want %q", in, got.State, want)
		}
	}
	if st := ParseStatusOutput(`{"192.0.2.2":{"bgpState":"Established","bgpTimerUpMsec":90000}}`); st.Uptime != 90 {
		t.Errorf("FRR uptime = %d, want 90", st.Uptime)
	}
}

func TestParseRoutesOutput(t *testing.T) {
	gobgp := `   Network              Next Hop             AS_PATH              Age        Attrs
*> 198.51.100.10/32     192.0.2.254                               00:01:02   [{Origin: ?} {Communities: 65535:666, no-export}]
*> 198.51.100.20/32     192.0.2.254                               00:00:10   [{Origin: ?} {Communities: 65535:666, no-export}]
`
	bird := `Table master4:
198.51.100.30/32     blackhole [rtbh_static 12:00:00.000] * (200)
2001:db8:100::1/128  blackhole [rtbh_static 12:00:00.000] * (200)
`
	frrJSON := `{"routes":{"198.51.100.40/32":[{"valid":true}],"::ffff:198.51.100.41/128":[{"valid":true}]}}`
	cases := map[string][]string{
		gobgp:                               {"198.51.100.10/32", "198.51.100.20/32"},
		bird:                                {"198.51.100.30/32", "2001:db8:100::1/128"},
		frrJSON:                             {"198.51.100.40/32", "198.51.100.41/32"},
		"":                                  nil,
		"date 10/27 12:00 and 192.0.2.1/33": nil,
	}
	for in, want := range cases {
		var got []string
		for _, p := range ParseRoutesOutput(in) {
			got = append(got, p.String())
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ParseRoutesOutput(%q) = %v, want %v", in, got, want)
		}
	}
}
