package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env, ListenAddr, PublicURL, DatabaseURL, RedisURL, WebDir                                       string
	DevMode, Demo, CookieSecure, SetupMode                                                          bool
	SessionTTL                                                                                      time.Duration
	AppSecret                                                                                       string
	SMTPHost                                                                                        string
	SMTPPort                                                                                        int
	SMTPUsername, SMTPPassword, SMTPFrom                                                            string
	StorageDriver, StorageDir, S3Endpoint, S3Region, S3Bucket, S3AccessKey, S3SecretKey             string
	MaxUploadBytes, MaxMediaBytes                                                                   int64
	FFprobePath                                                                                     string
	Sub2APIOrigin                                                                                   string
	MediaKeys                                                                                       map[string]string
	ImageCredential, VideoCredential                                                                string
	MediaOutputOrigins                                                                              []string
	MediaPollInterval, WorkerLease, TaskReviewAfter                                                 time.Duration
	EasyPayOrigin, EasyPayPID, EasyPayKey, EasyPayNotifyURL, EasyPayReturnURL, EasyPayCredentialRef string
	EasyPayKeys                                                                                     map[string]string
	BootstrapAdminEmail, BootstrapAdminPassword                                                     string
	TrustedProxyCIDRs                                                                               []netip.Prefix
}

func Load() (Config, error) {
	c := Config{
		Env: env("APP_ENV", "development"), ListenAddr: env("LISTEN_ADDR", "127.0.0.1:8090"), PublicURL: strings.TrimRight(env("PUBLIC_URL", "http://127.0.0.1:5176"), "/"),
		DatabaseURL: os.Getenv("DATABASE_URL"), RedisURL: os.Getenv("REDIS_URL"), WebDir: env("WEB_DIR", "../web/dist"), AppSecret: os.Getenv("APP_SECRET"),
		SessionTTL: 14 * 24 * time.Hour, StorageDriver: env("STORAGE_DRIVER", "local"), StorageDir: env("STORAGE_DIR", "../.runtime/media"),
		SMTPHost: os.Getenv("SMTP_HOST"), SMTPPort: number("SMTP_PORT", 587), SMTPUsername: os.Getenv("SMTP_USERNAME"), SMTPPassword: os.Getenv("SMTP_PASSWORD"), SMTPFrom: os.Getenv("SMTP_FROM"),
		S3Endpoint: os.Getenv("S3_ENDPOINT"), S3Region: env("S3_REGION", "auto"), S3Bucket: os.Getenv("S3_BUCKET"), S3AccessKey: os.Getenv("S3_ACCESS_KEY"), S3SecretKey: os.Getenv("S3_SECRET_KEY"),
		MaxUploadBytes: int64(number("MAX_UPLOAD_MIB", 256)) << 20, MaxMediaBytes: int64(number("MAX_MEDIA_MIB", 512)) << 20, FFprobePath: env("FFPROBE_PATH", "ffprobe"),
		Sub2APIOrigin: strings.TrimRight(os.Getenv("SUB2API_ORIGIN"), "/"), MediaKeys: map[string]string{}, ImageCredential: env("IMAGE_CREDENTIAL_REF", "image-v1"), VideoCredential: env("VIDEO_CREDENTIAL_REF", "video-v1"),
		MediaPollInterval: 5 * time.Second, WorkerLease: 2 * time.Minute, TaskReviewAfter: 24 * time.Hour,
		EasyPayOrigin: strings.TrimRight(os.Getenv("EASYPAY_ORIGIN"), "/"), EasyPayPID: os.Getenv("EASYPAY_PID"), EasyPayKey: os.Getenv("EASYPAY_KEY"), EasyPayCredentialRef: env("EASYPAY_CREDENTIAL_REF", "easypay-v1"), EasyPayKeys: map[string]string{},
		BootstrapAdminEmail: os.Getenv("BOOTSTRAP_ADMIN_EMAIL"), BootstrapAdminPassword: os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
	}
	c.DevMode = c.Env == "development" || c.Env == "test"
	c.Demo = os.Getenv("DEMO_MODE") == "true"
	c.SetupMode = os.Getenv("APP_SETUP_MODE") == "true"
	c.CookieSecure = !c.DevMode
	c.EasyPayNotifyURL = env("EASYPAY_NOTIFY_URL", c.PublicURL+"/api/v1/payment/notify/easypay")
	c.EasyPayReturnURL = env("EASYPAY_RETURN_URL", c.PublicURL+"/wallet")
	if raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS")); raw != "" {
		for _, value := range strings.Split(raw, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
			if err != nil {
				return c, errors.New("TRUSTED_PROXY_CIDRS must be a comma-separated list of IP CIDRs")
			}
			c.TrustedProxyCIDRs = append(c.TrustedProxyCIDRs, prefix.Masked())
		}
	}
	for name, target := range map[string]*map[string]string{"MEDIA_KEYS_JSON": &c.MediaKeys, "EASYPAY_KEYS_JSON": &c.EasyPayKeys} {
		if raw := os.Getenv(name); raw != "" {
			if err := json.Unmarshal([]byte(raw), target); err != nil {
				return c, fmt.Errorf("%s must be a JSON string map", name)
			}
		}
	}
	if key := os.Getenv("SUB2API_IMAGE_KEY"); key != "" {
		c.MediaKeys[c.ImageCredential] = key
	}
	if key := os.Getenv("SUB2API_VIDEO_KEY"); key != "" {
		c.MediaKeys[c.VideoCredential] = key
	}
	if c.EasyPayKey != "" {
		c.EasyPayKeys[c.EasyPayCredentialRef] = c.EasyPayKey
	}
	for _, origin := range strings.Split(os.Getenv("MEDIA_OUTPUT_ORIGINS"), ",") {
		if v := strings.TrimSpace(origin); v != "" {
			c.MediaOutputOrigins = append(c.MediaOutputOrigins, v)
		}
	}
	var err error
	c.StorageDir, err = filepath.Abs(c.StorageDir)
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	for _, prefix := range c.TrustedProxyCIDRs {
		if !prefix.IsValid() {
			return errors.New("TRUSTED_PROXY_CIDRS contains an invalid IP CIDR")
		}
	}
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required; use the isolated development database or configure PostgreSQL")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("PUBLIC_URL must be an HTTP(S) origin")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("PUBLIC_URL must not contain credentials, a path, query or fragment")
	}
	if len(c.AppSecret) < 32 {
		return errors.New("APP_SECRET must contain at least 32 characters")
	}
	if !c.DevMode && (u.Scheme != "https" || c.Demo) {
		return errors.New("production requires HTTPS and forbids DEMO_MODE")
	}
	if c.StorageDriver != "local" && c.StorageDriver != "s3" {
		return errors.New("STORAGE_DRIVER must be local or s3")
	}
	if !c.DevMode && c.StorageDriver != "s3" {
		return errors.New("production requires private S3/R2 storage")
	}
	if !c.SetupMode && c.StorageDriver == "s3" && (c.S3Bucket == "" || c.S3AccessKey == "" || c.S3SecretKey == "") {
		return errors.New("S3 bucket and credentials are required")
	}
	if !c.SetupMode && !c.DevMode && (c.SMTPHost == "" || c.SMTPFrom == "") {
		return errors.New("production requires SMTP for account verification")
	}
	if c.MaxUploadBytes <= 0 || c.MaxMediaBytes <= 0 {
		return errors.New("media size limits must be positive")
	}
	if c.Sub2APIOrigin != "" {
		g, e := url.Parse(c.Sub2APIOrigin)
		if e != nil || g.Host == "" || g.Scheme != "https" || g.User != nil || g.RawQuery != "" || g.Fragment != "" || (g.Path != "" && g.Path != "/") {
			return errors.New("SUB2API_ORIGIN must be a fixed HTTPS origin")
		}
	}
	for _, origin := range c.MediaOutputOrigins {
		v, e := url.Parse(origin)
		if e != nil || v.Host == "" || v.Scheme != "https" || v.User != nil || v.RawQuery != "" || v.Fragment != "" || (v.Path != "" && v.Path != "/") {
			return errors.New("MEDIA_OUTPUT_ORIGINS must contain fixed HTTPS origins")
		}
	}
	if !c.DevMode && c.S3Endpoint != "" {
		v, e := url.Parse(c.S3Endpoint)
		if e != nil || v.Scheme != "https" || v.Host == "" || v.User != nil || v.RawQuery != "" || v.Fragment != "" {
			return errors.New("production S3_ENDPOINT requires HTTPS without credentials or query")
		}
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func number(key string, fallback int) int {
	if raw := os.Getenv(key); raw != "" {
		n, e := strconv.Atoi(raw)
		if e == nil {
			return n
		}
		return -1
	}
	return fallback
}
