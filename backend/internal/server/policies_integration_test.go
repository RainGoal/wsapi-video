package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"wsapi-video/internal/config"
	"wsapi-video/internal/settings"
	"wsapi-video/internal/storage"
)

func sitePolicyFields(site *settings.Site) map[string]*string {
	return map[string]*string{
		"retention_policy": &site.RetentionPolicy, "storage_quota_policy": &site.StorageQuotaPolicy,
		"cleanup_policy": &site.CleanupPolicy, "submission_recovery_policy": &site.SubmissionRecoveryPolicy,
		"refund_policy": &site.RefundPolicy,
	}
}

func TestSitePoliciesSaveValidateAuditAndPublishWithoutSecrets(t *testing.T) {
	db := serverIntegrationDB(t)
	ctx := context.Background()
	srv := httptest.NewUnstartedServer(nil)
	origin := "http://" + srv.Listener.Addr().String()
	cfg := config.Config{
		Env: "test", DevMode: true, PublicURL: origin, AppSecret: strings.Repeat("policy-master", 4),
		StorageDriver: "local", StorageDir: t.TempDir(), WebDir: t.TempDir(),
		MaxUploadBytes: 8 << 20, MaxMediaBytes: 8 << 20, SessionTTL: time.Hour,
		Sub2APIOrigin: "https://media-policy.invalid", ImageCredential: "policy-image-ref", VideoCredential: "policy-video-ref",
		MediaKeys:     map[string]string{"policy-image-ref": "policy-image-secret", "policy-video-ref": "policy-video-secret"},
		EasyPayOrigin: "https://payment-policy.invalid", EasyPayPID: "policy-merchant-private", EasyPayKey: "policy-payment-secret",
		EasyPayCredentialRef: "policy-payment-ref", EasyPayNotifyURL: origin + "/api/v1/payment/notify/easypay", EasyPayReturnURL: origin + "/wallet",
		SMTPHost: "smtp-policy.invalid", SMTPPort: 587, SMTPUsername: "policy-smtp-user", SMTPPassword: "policy-smtp-secret", SMTPFrom: "sender@example.test",
		S3Endpoint: "https://storage-policy.invalid", S3Region: "auto", S3Bucket: "policy-private-bucket", S3AccessKey: "policy-storage-access", S3SecretKey: "policy-storage-secret",
	}
	blobs, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg, db, blobs)
	srv.Config.Handler = app.HTTP.Handler
	srv.Start()
	defer srv.Close()
	if err = app.Auth.BootstrapAdmin(ctx, "policy-admin@example.test", "Admin-policy-password-2026"); err != nil {
		t.Fatal(err)
	}
	admin, anonymous := newStudioClient(t, origin), newStudioClient(t, origin)
	admin.login("policy-admin@example.test", "Admin-policy-password-2026")
	assertPublic := func(site settings.Site) {
		t.Helper()
		raw := anonymous.json("GET", "/api/v1/system", nil, 200)
		public := serverData[map[string]json.RawMessage](t, raw)
		allowed := map[string]bool{"brand": true, "version": true, "demo": true, "development": true, "support_email": true,
			"registration_enabled": true, "storage_driver": true, "payment_enabled": true, "media_configured": true}
		var development bool
		if err := json.Unmarshal(public["development"], &development); err != nil || development != cfg.DevMode {
			t.Fatal("public runtime mode does not match actual configuration")
		}
		for key, want := range sitePolicyFields(&site) {
			allowed[key] = true
			var got string
			if value, ok := public[key]; !ok || json.Unmarshal(value, &got) != nil || got != *want {
				t.Fatalf("public %s does not match saved plain text", key)
			}
		}
		for key := range public {
			if !allowed[key] {
				t.Fatalf("unexpected public configuration field %s", key)
			}
		}
		for _, private := range []string{cfg.AppSecret, cfg.Sub2APIOrigin, cfg.ImageCredential, cfg.VideoCredential,
			cfg.MediaKeys[cfg.ImageCredential], cfg.MediaKeys[cfg.VideoCredential], cfg.EasyPayOrigin, cfg.EasyPayPID,
			cfg.EasyPayKey, cfg.EasyPayCredentialRef, cfg.SMTPHost, cfg.SMTPUsername, cfg.SMTPPassword,
			cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKey, cfg.S3SecretKey} {
			if bytes.Contains(raw, []byte(private)) {
				t.Fatal("public system output exposed private configuration")
			}
		}
	}
	v := serverData[settings.View](t, admin.json("GET", "/api/v1/admin/settings", nil, 200))
	for field, value := range sitePolicyFields(&v.Settings.Site) {
		if *value != "" {
			t.Fatalf("default %s policy is not empty", field)
		}
	}
	assertPublic(v.Settings.Site)
	u := settings.Update{Revision: v.Revision, Reason: "Publish test policy declarations", Settings: v.Settings}
	for field, value := range sitePolicyFields(&u.Settings.Site) {
		*value = "\u3000 \n测试说明：" + field + "\n保留纯文本 <b>标记</b>。\t\u3000"
	}
	// A 2000-code-point text exceeds 2000 UTF-8 bytes and includes a supplementary rune.
	u.Settings.Site.StorageQuotaPolicy = " \u3000" + strings.Repeat("界", 1999) + "🙂\n "
	v = serverData[settings.View](t, admin.json("PUT", "/api/v1/admin/settings", u, 200))
	if v.Revision != 1 {
		t.Fatal("policy save did not advance the configuration revision", v.Revision)
	}
	want := u.Settings.Site
	for _, value := range sitePolicyFields(&want) {
		*value = strings.TrimSpace(*value)
	}
	if v.Settings.Site != want {
		t.Fatal("policy whitespace was not trimmed, or plain text changed")
	}
	assertPublic(want)
	admin.json("PUT", "/api/v1/admin/settings", u, 409)
	for field := range sitePolicyFields(&v.Settings.Site) {
		t.Run(field+"_overflow", func(t *testing.T) {
			invalid := settings.Update{Revision: v.Revision, Reason: "Reject policy beyond Unicode limit", Settings: v.Settings}
			*sitePolicyFields(&invalid.Settings.Site)[field] = strings.Repeat("界", 2000) + "🙂"
			raw := admin.json("PUT", "/api/v1/admin/settings", invalid, 422)
			var failure struct {
				Code string `json:"code"`
				Data struct {
					Fields map[string]string `json:"fields"`
				} `json:"data"`
			}
			if err := json.Unmarshal(raw, &failure); err != nil || failure.Code != "invalid_settings" || failure.Data.Fields["site."+field] == "" || len(failure.Data.Fields) != 1 {
				t.Fatal("policy overflow did not return its field error", string(raw), err)
			}
		})
	}
	unchanged := serverData[settings.View](t, admin.json("GET", "/api/v1/admin/settings", nil, 200))
	if unchanged.Revision != v.Revision || unchanged.Settings.Site != want {
		t.Fatal("rejected policy update changed the saved configuration")
	}
	var auditCount int
	if err = db.QueryRow(ctx, "SELECT count(*) FROM admin_audit WHERE action='settings.update'").Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("rejected or conflicting policy updates wrote an audit entry", auditCount, err)
	}
	var auditRaw []byte
	if err = db.QueryRow(ctx, "SELECT details FROM admin_audit WHERE action='settings.update'").Scan(&auditRaw); err != nil {
		t.Fatal(err)
	}
	var audit map[string]json.RawMessage
	if err = json.Unmarshal(auditRaw, &audit); err != nil || len(audit) != 2 {
		t.Fatal("policy audit must only contain revision and changed field names", err)
	}
	var changed []string
	var auditRevision int64
	if err = json.Unmarshal(audit["changed_fields"], &changed); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(audit["revision"], &auditRevision); err != nil {
		t.Fatal(err)
	}
	wantChanged := []string{}
	for field := range sitePolicyFields(&want) {
		wantChanged = append(wantChanged, "site."+field)
	}
	sort.Strings(wantChanged)
	if auditRevision != v.Revision || !reflect.DeepEqual(changed, wantChanged) {
		t.Fatal("policy audit did not record all changed policy fields", changed)
	}
	u = settings.Update{Revision: v.Revision, Reason: "Clear test policy declarations", Settings: v.Settings}
	for _, value := range sitePolicyFields(&u.Settings.Site) {
		*value = " \n\u3000"
	}
	v = serverData[settings.View](t, admin.json("PUT", "/api/v1/admin/settings", u, 200))
	for field, value := range sitePolicyFields(&v.Settings.Site) {
		if *value != "" {
			t.Fatalf("blank %s policy did not clear the saved text", field)
		}
	}
	assertPublic(v.Settings.Site)
}
