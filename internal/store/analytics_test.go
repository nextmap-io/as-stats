package store

import "testing"

// TestOrderColumnForMetric verifies the F1 metric whitelist maps known metrics
// to their aggregated columns and falls back to total_bytes for anything else.
func TestOrderColumnForMetric(t *testing.T) {
	cases := map[string]string{
		"bytes":   "total_bytes",
		"packets": "total_packets",
		"flows":   "total_flows",
		// Unknown / hostile inputs must all collapse to the safe default.
		"":                               "total_bytes",
		"BYTES":                          "total_bytes",
		"conns":                          "total_bytes",
		"total_bytes DESC; DROP TABLE t": "total_bytes",
		"flow_count":                     "total_bytes",
	}
	for in, want := range cases {
		if got := orderColumnForMetric(in); got != want {
			t.Errorf("orderColumnForMetric(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestOrderColumnForMetricNeverEchoesInput guards the injection surface: the
// resolved column must always be one of the three known columns.
func TestOrderColumnForMetricNeverEchoesInput(t *testing.T) {
	allowed := map[string]bool{"total_bytes": true, "total_packets": true, "total_flows": true}
	for _, in := range []string{"'; --", "packets)", "bytes OR 1=1", "../etc"} {
		if got := orderColumnForMetric(in); !allowed[got] {
			t.Errorf("orderColumnForMetric(%q) returned non-whitelisted column %q", in, got)
		}
	}
}

// TestConvDimensionFor verifies the F3 conversation dimension whitelist accepts
// exactly the three known dimensions and rejects everything else.
func TestConvDimensionFor(t *testing.T) {
	valid := []string{"src_dst_ip", "src_dst_as", "dst_port_proto"}
	for _, dim := range valid {
		d, ok := convDimensionFor(dim)
		if !ok {
			t.Errorf("convDimensionFor(%q) = not ok, want ok", dim)
			continue
		}
		if d.aExpr == "" || d.bExpr == "" || d.fwdExpr == "" {
			t.Errorf("convDimensionFor(%q) returned incomplete mapping: %+v", dim, d)
		}
	}

	for _, dim := range []string{"", "SRC_DST_IP", "src_ip", "dst_port", "src_dst_ip; --", "protocol"} {
		if _, ok := convDimensionFor(dim); ok {
			t.Errorf("convDimensionFor(%q) = ok, want rejected", dim)
		}
	}
}

// TestConvDimensionCleanIP confirms only the IP dimension strips the mapped
// IPv6 prefix (AS / port-proto endpoints are numeric and must not be touched).
func TestConvDimensionCleanIP(t *testing.T) {
	if d, _ := convDimensionFor("src_dst_ip"); !d.cleanIP {
		t.Error("src_dst_ip should set cleanIP")
	}
	if d, _ := convDimensionFor("src_dst_as"); d.cleanIP {
		t.Error("src_dst_as should not set cleanIP")
	}
	if d, _ := convDimensionFor("dst_port_proto"); d.cleanIP {
		t.Error("dst_port_proto should not set cleanIP")
	}
}

// TestClassifyAsymmetry checks the F2 traffic-class thresholds.
func TestClassifyAsymmetry(t *testing.T) {
	cases := []struct {
		name      string
		in, out   uint64
		wantClass string
	}{
		{"content_heavy_egress", 1_000, 100_000, "content"},
		{"eyeball_heavy_ingress", 100_000, 1_000, "eyeball"},
		{"balanced", 1_000, 1_000, "balanced"},
		{"zero_in_treated_as_one", 0, 100, "content"},
		{"all_zero_balanced", 0, 0, "balanced"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ratio, class := classifyAsymmetry(c.in, c.out)
			if class != c.wantClass {
				t.Errorf("classifyAsymmetry(%d,%d) class = %q, want %q (ratio=%f)", c.in, c.out, class, c.wantClass, ratio)
			}
		})
	}
}
