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
// Example: "(isIPAddressInRange(toString(ip), '192.0.2.0/24') OR isIPAddressInRange(toString(ip), '2001:db8::/32'))"
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

// PrivateIPFilter returns a SQL expression that matches RFC1918, CGNAT, link-local IPs.
func PrivateIPFilter(col string) string {
	return fmt.Sprintf(`(
		isIPAddressInRange(replaceRegexpOne(toString(%s), '^::ffff:', ''), '10.0.0.0/8') OR
		isIPAddressInRange(replaceRegexpOne(toString(%s), '^::ffff:', ''), '172.16.0.0/12') OR
		isIPAddressInRange(replaceRegexpOne(toString(%s), '^::ffff:', ''), '192.168.0.0/16') OR
		isIPAddressInRange(replaceRegexpOne(toString(%s), '^::ffff:', ''), '100.64.0.0/10') OR
		isIPAddressInRange(replaceRegexpOne(toString(%s), '^::ffff:', ''), '169.254.0.0/16') OR
		isIPAddressInRange(toString(%s), 'fe80::/10') OR
		isIPAddressInRange(toString(%s), 'fc00::/7')
	)`, col, col, col, col, col, col, col)
}
