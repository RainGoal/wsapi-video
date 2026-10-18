// Package settings owns encrypted, revisioned runtime settings for the sole
// Sub2API connector and EasyPay merchant. Deployment trust roots stay in env.
package settings

import "time"

type Site struct {
	Brand                    string `json:"brand"`
	RegistrationEnabled      bool   `json:"registration_enabled"`
	SupportEmail             string `json:"support_email"`
	RetentionPolicy          string `json:"retention_policy"`
	StorageQuotaPolicy       string `json:"storage_quota_policy"`
	CleanupPolicy            string `json:"cleanup_policy"`
	SubmissionRecoveryPolicy string `json:"submission_recovery_policy"`
	RefundPolicy             string `json:"refund_policy"`
}
type Media struct {
	Origin             string   `json:"origin"`
	OutputOrigins      []string `json:"output_origins"`
	ImageKey           string   `json:"image_key,omitempty"`
	VideoKey           string   `json:"video_key,omitempty"`
	ImageKeyConfigured bool     `json:"image_key_configured"`
	VideoKeyConfigured bool     `json:"video_key_configured"`
}
type Payment struct {
	Enabled       bool   `json:"enabled"`
	Origin        string `json:"origin"`
	PID           string `json:"pid"`
	NotifyURL     string `json:"notify_url"`
	ReturnURL     string `json:"return_url"`
	Key           string `json:"key,omitempty"`
	KeyConfigured bool   `json:"key_configured"`
}
type SMTP struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	Password           string `json:"password,omitempty"`
	From               string `json:"from"`
	PasswordConfigured bool   `json:"password_configured"`
}
type Storage struct {
	Driver              string `json:"driver"`
	Endpoint            string `json:"endpoint"`
	Region              string `json:"region"`
	Bucket              string `json:"bucket"`
	AccessKey           string `json:"access_key,omitempty"`
	SecretKey           string `json:"secret_key,omitempty"`
	AccessKeyConfigured bool   `json:"access_key_configured"`
	SecretKeyConfigured bool   `json:"secret_key_configured"`
	LocationLocked      bool   `json:"location_locked"`
}
type Settings struct {
	Site    Site    `json:"site"`
	Media   Media   `json:"media"`
	Payment Payment `json:"payment"`
	SMTP    SMTP    `json:"smtp"`
	Storage Storage `json:"storage"`
}
type State struct {
	Settings   Settings   `json:"settings"`
	ImageRef   string     `json:"image_ref"`
	VideoRef   string     `json:"video_ref"`
	PaymentRef string     `json:"payment_ref"`
	Revision   int64      `json:"revision"`
	UpdatedAt  *time.Time `json:"updated_at"`
}
type Readiness struct {
	Media        bool `json:"media"`
	Payment      bool `json:"payment"`
	SMTP         bool `json:"smtp"`
	Storage      bool `json:"storage"`
	Registration bool `json:"registration"`
	Demo         bool `json:"demo"`
}
type View struct {
	Revision  int64      `json:"revision"`
	UpdatedAt *time.Time `json:"updated_at"`
	Settings  Settings   `json:"settings"`
	Readiness Readiness  `json:"readiness"`
}
type Update struct {
	Revision     int64    `json:"revision"`
	Reason       string   `json:"reason"`
	Settings     Settings `json:"settings"`
	ClearSecrets []string `json:"clear_secrets,omitempty"`
}
