package auth

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"wsapi-video/internal/config"
)

func TestRateLimitIPTrustsOnlyConfiguredDirectProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("192.0.2.10/32"), netip.MustParsePrefix("2001:db8:1::/64")}
	tests := []struct {
		name, peer string
		headers    []string
		trust      bool
		want       string
	}{
		{"default ignores header", "192.0.2.10:1234", []string{"198.51.100.1"}, false, "192.0.2.10"},
		{"untrusted peer ignores header", "192.0.2.11:1234", []string{"198.51.100.1"}, true, "192.0.2.11"},
		{"trusted IPv4", "192.0.2.10:1234", []string{"198.51.100.1"}, true, "198.51.100.1"},
		{"different client has separate bucket", "192.0.2.10:1234", []string{"198.51.100.2"}, true, "198.51.100.2"},
		{"trusted IPv6", "[2001:db8:1::10]:1234", []string{"2001:db8:2::1"}, true, "2001:db8:2::1"},
		{"mapped peer matches IPv4 CIDR", "[::ffff:192.0.2.10]:1234", []string{"::ffff:198.51.100.1"}, true, "198.51.100.1"},
		{"missing header ignores XFF", "192.0.2.10:1234", nil, true, "192.0.2.10"},
		{"multiple headers", "192.0.2.10:1234", []string{"198.51.100.1", "198.51.100.2"}, true, "192.0.2.10"},
		{"comma chain", "192.0.2.10:1234", []string{"198.51.100.1, 198.51.100.2"}, true, "192.0.2.10"},
		{"host name", "192.0.2.10:1234", []string{"client.example.com"}, true, "192.0.2.10"},
		{"port", "192.0.2.10:1234", []string{"198.51.100.1:80"}, true, "192.0.2.10"},
		{"IPv6 brackets", "192.0.2.10:1234", []string{"[2001:db8:2::1]"}, true, "192.0.2.10"},
		{"zone", "192.0.2.10:1234", []string{"fe80::1%eth0"}, true, "192.0.2.10"},
		{"whitespace", "192.0.2.10:1234", []string{" 198.51.100.1 "}, true, "192.0.2.10"},
		{"invalid IP", "192.0.2.10:1234", []string{"300.51.100.1"}, true, "192.0.2.10"},
		{"empty header", "192.0.2.10:1234", []string{""}, true, "192.0.2.10"},
		{"malformed peer", "192.0.2.10", []string{"198.51.100.1"}, true, "192.0.2.10"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := &Service{cfg: config.Config{}}
			if test.trust {
				s.cfg.TrustedProxyCIDRs = trusted
			}
			r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
			r.RemoteAddr = test.peer
			for _, value := range test.headers {
				r.Header.Add("X-Real-IP", value)
			}
			r.Header.Set("X-Forwarded-For", "203.0.113.99")
			if got := s.rateLimitIP(r); got != test.want {
				t.Fatalf("rate limit IP = %q, want %q", got, test.want)
			}
		})
	}
}
