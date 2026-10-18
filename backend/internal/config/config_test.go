package config

import (
	"net/netip"
	"strings"
	"testing"
)

func TestProductionRejectsUnsafeConfiguration(t *testing.T) {
	valid := Config{PublicURL: "https://studio.example.com", DatabaseURL: "postgres://example", AppSecret: strings.Repeat("s", 32), StorageDriver: "s3", S3Bucket: "private", S3AccessKey: "id", S3SecretKey: "secret", SMTPHost: "smtp.example.com", SMTPFrom: "studio@example.com", MaxUploadBytes: 100, MaxMediaBytes: 100}
	if e := valid.Validate(); e != nil {
		t.Fatal(e)
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"demo", func(c *Config) { c.Demo = true }}, {"http", func(c *Config) { c.PublicURL = "http://studio.example.com" }},
		{"local storage", func(c *Config) { c.StorageDriver = "local" }}, {"short secret", func(c *Config) { c.AppSecret = "short" }},
		{"arbitrary gateway path", func(c *Config) { c.Sub2APIOrigin = "https://gateway.example.com/custom" }},
		{"gateway credentials", func(c *Config) { c.Sub2APIOrigin = "https://key@gateway.example.com" }},
		{"insecure media output", func(c *Config) { c.MediaOutputOrigins = []string{"http://cdn.example.com"} }},
		{"media output path", func(c *Config) { c.MediaOutputOrigins = []string{"https://cdn.example.com/files"} }},
		{"insecure S3", func(c *Config) { c.S3Endpoint = "http://s3.example.com" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := valid
			test.mutate(&c)
			if e := c.Validate(); e == nil {
				t.Fatal("unsafe production configuration accepted")
			}
		})
	}
}

func TestProductionSetupOnlyRelaxesExternalDependencyCompleteness(t *testing.T) {
	c := Config{PublicURL: "https://studio.example.com", DatabaseURL: "postgres://example", AppSecret: strings.Repeat("s", 32), StorageDriver: "s3", MaxUploadBytes: 100, MaxMediaBytes: 100, SetupMode: true}
	if err := c.Validate(); err != nil {
		t.Fatal("administrator setup must be able to start before external configuration", err)
	}
	c.Demo = true
	if err := c.Validate(); err == nil {
		t.Fatal("setup allowed production demo mode")
	}
	c.Demo = false
	c.StorageDriver = "local"
	if err := c.Validate(); err == nil {
		t.Fatal("setup allowed public local storage in production")
	}
}

func TestLoadTrustedProxyCIDRs(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://unused")
	t.Setenv("APP_SECRET", strings.Repeat("s", 32))
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8090")
	t.Setenv("STORAGE_DRIVER", "local")
	t.Setenv("MAX_UPLOAD_MIB", "256")
	t.Setenv("MAX_MEDIA_MIB", "512")
	for _, key := range []string{"MEDIA_KEYS_JSON", "EASYPAY_KEYS_JSON", "MEDIA_OUTPUT_ORIGINS", "SUB2API_ORIGIN"} {
		t.Setenv(key, "")
	}
	for _, test := range []struct {
		name, raw string
		want      []string
		invalid   bool
	}{
		{"default", "", nil, false},
		{"explicit proxies", "192.0.2.10/32, 2001:db8:1::123/64", []string{"192.0.2.10/32", "2001:db8:1::/64"}, false},
		{"bare IP", "192.0.2.10", nil, true},
		{"bad prefix", "192.0.2.10/33", nil, true},
		{"host name", "proxy.example.com/32", nil, true},
		{"trailing empty entry", "192.0.2.10/32,", nil, true},
		{"mixed valid and invalid", "192.0.2.10/32,invalid", nil, true},
		{"IPv6 zone", "fe80::1%eth0/64", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXY_CIDRS", test.raw)
			c, err := Load()
			if test.invalid {
				if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
					t.Fatalf("invalid proxy config must reject startup: %v", err)
				}
				return
			}
			if err != nil || len(c.TrustedProxyCIDRs) != len(test.want) {
				t.Fatalf("unexpected trusted proxies: %v %v", c.TrustedProxyCIDRs, err)
			}
			for i, prefix := range c.TrustedProxyCIDRs {
				if prefix.String() != test.want[i] {
					t.Fatalf("proxy %d = %q, want %q", i, prefix, test.want[i])
				}
			}
		})
	}
	if err := (Config{TrustedProxyCIDRs: []netip.Prefix{{}}}).Validate(); err == nil {
		t.Fatal("invalid programmatic prefix accepted")
	}
}
