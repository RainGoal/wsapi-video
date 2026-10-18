package commerce

import (
	"context"
	"strings"
	"testing"

	"wsapi-video/internal/config"
	"wsapi-video/internal/settings"
)

func TestPostgresLegacyEnvironmentCredentialUpgrade(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	actor := seedUser(t, db, 0)
	cfg := config.Config{
		DevMode: true, PublicURL: "https://studio.example", AppSecret: strings.Repeat("upgrade", 6),
		StorageDriver: "local", SMTPPort: 587, Sub2APIOrigin: "https://legacy-media.example",
		ImageCredential: "current-image", VideoCredential: "current-video",
		MediaKeys:     map[string]string{"current-image": "current-image-key", "current-video": "current-video-key", "legacy-media": "legacy-media-key"},
		EasyPayOrigin: "https://legacy-pay.example/epay", EasyPayPID: "legacy-merchant", EasyPayCredentialRef: "current-payment",
		EasyPayKeys:      map[string]string{"current-payment": "current-payment-key", "legacy-payment": "legacy-payment-key"},
		EasyPayNotifyURL: "https://studio.example/api/v1/payment/notify/easypay", EasyPayReturnURL: "https://studio.example/wallet",
	}
	runtime := settings.New(db, cfg)
	assertLegacy := func() {
		t.Helper()
		c, err := runtime.CredentialConfig(ctx, "legacy-media", "media")
		if err != nil || c.Sub2APIOrigin != cfg.Sub2APIOrigin || c.MediaKeys["legacy-media"] != "legacy-media-key" {
			t.Fatal("legacy media credential lost during upgrade", err)
		}
		c, err = runtime.CredentialConfig(ctx, "legacy-payment", "payment")
		if err != nil || c.EasyPayOrigin != cfg.EasyPayOrigin || c.EasyPayPID != cfg.EasyPayPID || c.EasyPayKeys["legacy-payment"] != "legacy-payment-key" {
			t.Fatal("legacy payment credential lost during upgrade", err)
		}
		jobs := New(db, cfg, nil)
		jobs.SetRuntimeSettings(runtime)
		if _, err = jobs.paymentAdapterContext(ctx, "legacy-payment", cfg.EasyPayOrigin, cfg.EasyPayPID); err != nil {
			t.Fatal("legacy order cannot retain original merchant", err)
		}
		if _, err = jobs.paymentAdapterContext(ctx, "legacy-payment", "https://different-pay.example", cfg.EasyPayPID); err == nil {
			t.Fatal("legacy order accepted a different provider origin")
		}
		for _, origin := range []string{"https://legacy-pay.example", "https://legacy-pay.example/other-epay"} {
			if _, err = jobs.paymentAdapterContext(ctx, "legacy-payment", origin, cfg.EasyPayPID); err == nil {
				t.Fatal("legacy order accepted a different payment base path", origin)
			}
		}
		if _, err = jobs.paymentAdapterContext(ctx, "legacy-payment", cfg.EasyPayOrigin, "different-merchant"); err == nil {
			t.Fatal("legacy order accepted a different merchant")
		}
	}
	assertLegacy()
	for _, pair := range [][2]string{{"unknown", "media"}, {"unknown", "payment"}, {"legacy-payment", "media"}, {"legacy-media", "payment"}} {
		if _, err := runtime.CredentialConfig(ctx, pair[0], pair[1]); err == nil {
			t.Fatal("unconfigured credential accepted", pair)
		}
	}
	st, err := runtime.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u := settings.Update{Settings: st.Settings, Reason: "Rotate after legacy deployment upgrade"}
	u.Settings.Media.Origin = "https://new-media.example"
	u.Settings.Media.ImageKey = "new-image-key"
	u.Settings.Payment.Origin = "https://new-pay.example/epay/"
	u.Settings.Payment.PID = "new-merchant"
	u.Settings.Payment.Key = "new-payment-key"
	if err = runtime.Save(ctx, actor, u); err != nil {
		t.Fatal(err)
	}
	assertLegacy()
	st, err = runtime.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u = settings.Update{Revision: st.Revision, Settings: st.Settings, Reason: "Explicitly clear current credentials", ClearSecrets: []string{"media.image_key", "payment.key"}}
	u.Settings.Payment.Enabled = false
	if err = runtime.Save(ctx, actor, u); err != nil {
		t.Fatal(err)
	}
	st, err = runtime.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Even an environment entry for a current ref must not resurrect its cleared key.
	cfg.MediaKeys[st.ImageRef] = "stale-environment-image-key"
	cfg.EasyPayKeys[st.PaymentRef] = "stale-environment-payment-key"
	if _, err = db.Exec(ctx, "DELETE FROM runtime_credential_versions WHERE credential_ref IN ($1,$2)", st.ImageRef, st.PaymentRef); err != nil {
		t.Fatal(err)
	}
	runtime = settings.New(db, cfg)
	for _, pair := range [][2]string{{st.ImageRef, "media"}, {st.PaymentRef, "payment"}} {
		c, e := runtime.CredentialConfig(ctx, pair[0], pair[1])
		if e != nil || c.MediaKeys[st.ImageRef] != "" || c.EasyPayKeys[st.PaymentRef] != "" {
			t.Fatal("cleared database credential fell back to environment", pair, e)
		}
	}
	if _, err = db.Exec(ctx, "INSERT INTO runtime_credential_versions(credential_ref,kind,payload) VALUES('legacy-media','media',$1)", []byte("invalid ciphertext")); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.CredentialConfig(ctx, "legacy-media", "media"); err == nil {
		t.Fatal("unreadable credential ciphertext fell back to environment")
	}
	if _, err = db.Exec(ctx, "UPDATE runtime_settings SET payload=$1", []byte("invalid ciphertext")); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.CredentialConfig(ctx, "legacy-payment", "payment"); err == nil {
		t.Fatal("unreadable runtime settings fell back to environment")
	}
}
