package ripestat

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// FetchASPrefixes retrieves the announced prefixes for an AS from RIPEstat.
func FetchASPrefixes(asn uint32) ([]net.IPNet, error) {
	url := fmt.Sprintf("https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS%d", asn)
	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("RIPEstat request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RIPEstat returned %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Prefixes []struct {
				Prefix string `json:"prefix"`
			} `json:"prefixes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode RIPEstat: %w", err)
	}

	var nets []net.IPNet
	for _, p := range result.Data.Prefixes {
		_, ipNet, err := net.ParseCIDR(p.Prefix)
		if err != nil {
			continue
		}
		nets = append(nets, *ipNet)
	}

	return nets, nil
}

// PrefixesToSQL returns a SQL OR expression for ClickHouse isIPAddressInRange().
// Example: "(isIPAddressInRange(toString(ip), '85.208.144.0/22') OR isIPAddressInRange(toString(ip), '2a09:8740::/29'))"
func PrefixesToSQL(col string, prefixes []net.IPNet) string {
	if len(prefixes) == 0 {
		return "1=1"
	}
	s := "("
	for i, p := range prefixes {
		if i > 0 {
			s += " OR "
		}
		s += fmt.Sprintf("isIPAddressInRange(replaceRegexpOne(toString(%s), '^::ffff:', ''), '%s')", col, p.String())
	}
	s += ")"
	return s
}
