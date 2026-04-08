package handler

import "testing"

func TestValidateWebhookURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Valid
		{"slack-https", "https://hooks.slack.com/services/T0/B0/xxx", false},
		{"discord-https", "https://discord.com/api/webhooks/123/abc", false},
		{"teams-https", "https://outlook.office.com/webhook/abc", false},
		{"http-public", "http://example.com/webhook", false},

		// Invalid scheme
		{"file-scheme", "file:///etc/passwd", true},
		{"javascript-scheme", "javascript:alert(1)", true},
		{"data-scheme", "data:text/plain,test", true},
		{"gopher-scheme", "gopher://example.com", true},

		// Loopback
		{"localhost-name", "http://localhost/webhook", true},
		{"localhost-127", "http://127.0.0.1/webhook", true},
		{"localhost-ipv6", "http://[::1]/webhook", true},
		{"127.x", "http://127.42.0.1/webhook", true},

		// Private RFC1918
		{"10.0.0.0/8", "http://10.0.0.1/webhook", true},
		{"172.16.0.0/12", "http://172.16.0.1/webhook", true},
		{"192.168.0.0/16", "http://192.168.1.1/webhook", true},

		// CGNAT
		{"cgnat-100.64", "http://100.64.0.1/webhook", true},
		{"cgnat-100.127", "http://100.127.255.254/webhook", true},
		// Just outside CGNAT
		{"public-100.63", "http://100.63.0.1/webhook", false},
		{"public-100.128", "http://100.128.0.1/webhook", false},

		// Link-local
		{"link-local-v4", "http://169.254.1.1/webhook", true},
		{"link-local-v6", "http://[fe80::1]/webhook", true},

		// Multicast
		{"multicast-v4", "http://224.0.0.1/webhook", true},

		// Unspecified
		{"unspec-v4", "http://0.0.0.0/webhook", true},
		{"unspec-v6", "http://[::]/webhook", true},

		// Local hostnames
		{".local", "http://server.local/webhook", true},
		{".localhost", "http://test.localhost/webhook", true},

		// Empty / malformed
		{"empty", "", true},
		{"missing-host", "http:///path", true},
		{"garbage", "::not a url::", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWebhookURL(tc.url)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tc.url, err)
			}
		})
	}
}
