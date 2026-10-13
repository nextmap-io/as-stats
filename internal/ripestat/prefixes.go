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
