package settings

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/media"
)

var ErrConflict = errors.New("settings revision conflict")
var ErrStorageUnavailable = fmt.Errorf("private storage is not configured: %w", media.ErrUnavailable)

type FieldErrors map[string]string

func (e FieldErrors) Error() string { return "invalid runtime settings" }

type Service struct {
	db   *pgxpool.Pool
	base config.Config
	aead cipher.AEAD
}

func New(db *pgxpool.Pool, cfg config.Config) *Service {
	key := sha256.Sum256([]byte("frame-studio:runtime-settings:v1:" + cfg.AppSecret))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	return &Service{db: db, base: cfg, aead: aead}
}
func (s *Service) seal(v any, binding string) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, raw, []byte(binding)), nil
}
func (s *Service) open(b []byte, binding string, v any) error {
	n := s.aead.NonceSize()
	if len(b) < n {
		return errors.New("invalid settings ciphertext")
	}
	raw, err := s.aead.Open(nil, b[:n], b[n:], []byte(binding))
	if err != nil {
		return errors.New("cannot decrypt runtime settings; check deployment master key")
	}
	return json.Unmarshal(raw, v)
}
func (s *Service) defaults() State {
	c := s.base
	key := c.EasyPayKeys[c.EasyPayCredentialRef]
	if key == "" {
		key = c.EasyPayKey
	}
	return State{ImageRef: c.ImageCredential, VideoRef: c.VideoCredential, PaymentRef: c.EasyPayCredentialRef, Settings: Settings{
		Site:    Site{Brand: "FRAME STUDIO", RegistrationEnabled: true},
		Media:   Media{Origin: c.Sub2APIOrigin, OutputOrigins: append([]string{}, c.MediaOutputOrigins...), ImageKey: c.MediaKeys[c.ImageCredential], VideoKey: c.MediaKeys[c.VideoCredential]},
		Payment: Payment{Enabled: c.EasyPayOrigin != "" && c.EasyPayPID != "" && key != "", Origin: c.EasyPayOrigin, PID: c.EasyPayPID, Key: key, NotifyURL: c.EasyPayNotifyURL, ReturnURL: c.EasyPayReturnURL},
		SMTP:    SMTP{Host: c.SMTPHost, Port: c.SMTPPort, Username: c.SMTPUsername, Password: c.SMTPPassword, From: c.SMTPFrom},
		Storage: Storage{Driver: c.StorageDriver, Endpoint: c.S3Endpoint, Region: c.S3Region, Bucket: c.S3Bucket, AccessKey: c.S3AccessKey, SecretKey: c.S3SecretKey},
	}}
}
func (s *Service) Current(ctx context.Context) (State, error) {
	var b []byte
	var revision int64
	var updated time.Time
	err := s.db.QueryRow(ctx, "SELECT payload,revision,updated_at FROM runtime_settings WHERE singleton").Scan(&b, &revision, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.defaults(), nil
	}
	if err != nil {
		return State{}, err
	}
	var out State
	err = s.open(b, "runtime-settings", &out)
	out.Revision = revision
	out.UpdatedAt = &updated
	return out, err
}
func (s *Service) Ready(st State) Readiness {
	v := st.Settings
	demo := s.base.DevMode && s.base.Demo
	r := Readiness{Demo: demo, Media: v.Media.Origin != "" && (v.Media.ImageKey != "" || v.Media.VideoKey != ""), SMTP: v.SMTP.Host != "" && v.SMTP.From != "", Storage: v.Storage.Driver == "local" && s.base.DevMode || v.Storage.Driver == "s3" && v.Storage.Bucket != "" && v.Storage.AccessKey != "" && v.Storage.SecretKey != ""}
	r.Payment = !demo && v.Payment.Enabled && v.Payment.Origin != "" && v.Payment.PID != "" && v.Payment.Key != ""
	r.Registration = v.Site.RegistrationEnabled && (s.base.DevMode || r.SMTP && r.Storage)
	return r
}
func (s *Service) Config(st State) config.Config {
	c := s.base
	v := st.Settings
	c.Sub2APIOrigin = v.Media.Origin
	c.MediaOutputOrigins = append([]string{}, v.Media.OutputOrigins...)
	c.ImageCredential = st.ImageRef
	c.VideoCredential = st.VideoRef
	c.MediaKeys = map[string]string{st.ImageRef: v.Media.ImageKey, st.VideoRef: v.Media.VideoKey}
	c.EasyPayOrigin = v.Payment.Origin
	c.EasyPayPID = v.Payment.PID
	c.EasyPayKey = v.Payment.Key
	c.EasyPayCredentialRef = st.PaymentRef
	c.EasyPayKeys = map[string]string{st.PaymentRef: v.Payment.Key}
	c.EasyPayNotifyURL = v.Payment.NotifyURL
	c.EasyPayReturnURL = v.Payment.ReturnURL
	c.SMTPHost = v.SMTP.Host
	c.SMTPPort = v.SMTP.Port
	c.SMTPUsername = v.SMTP.Username
	c.SMTPPassword = v.SMTP.Password
	c.SMTPFrom = v.SMTP.From
	c.StorageDriver = v.Storage.Driver
	c.S3Endpoint = v.Storage.Endpoint
	c.S3Region = v.Storage.Region
	c.S3Bucket = v.Storage.Bucket
	c.S3AccessKey = v.Storage.AccessKey
	c.S3SecretKey = v.Storage.SecretKey
	return c
}
func (s *Service) RuntimeConfig(ctx context.Context) (config.Config, error) {
	st, err := s.Current(ctx)
	if err != nil {
		return config.Config{}, err
	}
	return s.Config(st), nil
}
func (s *Service) forCredential(ctx context.Context, ref, kind string) (State, error) {
	var raw []byte
	err := s.db.QueryRow(ctx, "SELECT payload FROM runtime_credential_versions WHERE credential_ref=$1 AND kind=$2", ref, kind).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		st, e := s.Current(ctx)
		if e != nil {
			return State{}, e
		}
		if kind == "media" && (ref == st.ImageRef || ref == st.VideoRef) || kind == "payment" && ref == st.PaymentRef {
			return st, nil
		}
		// Before runtime settings existed, accepted work could refer to any
		// explicitly retained environment credential. Keep that upgrade path,
		// without replacing a current or unreadable database credential.
		legacy := s.defaults()
		if key := s.base.MediaKeys[ref]; kind == "media" && ref != "" && key != "" {
			legacy.ImageRef, legacy.VideoRef = ref, ref
			legacy.Settings.Media.ImageKey, legacy.Settings.Media.VideoKey = key, key
			return legacy, nil
		}
		if key := s.base.EasyPayKeys[ref]; kind == "payment" && ref != "" && key != "" {
			legacy.PaymentRef = ref
			legacy.Settings.Payment.Key = key
			return legacy, nil
		}
		return State{}, errors.New("original credential is unavailable")
	}
	if err != nil {
		return State{}, err
	}
	var st State
	err = s.open(raw, "credential:"+kind+":"+ref, &st)
	return st, err
}
func (s *Service) CredentialConfig(ctx context.Context, ref, kind string) (config.Config, error) {
	st, err := s.forCredential(ctx, ref, kind)
	if err != nil {
		return config.Config{}, err
	}
	return s.Config(st), nil
}
func (s *Service) Gateway(ctx context.Context, ref string) (*media.Connector, error) {
	var st State
	var err error
	if ref == "" {
		st, err = s.Current(ctx)
	} else {
		st, err = s.forCredential(ctx, ref, "media")
	}
	if err != nil {
		return nil, err
	}
	if ref == "" && !s.base.DevMode && !s.Ready(st).Storage {
		return nil, ErrStorageUnavailable
	}
	c := s.Config(st)
	if ref != "" {
		// Download permission follows the current administrator policy, while
		// the accepted task retains its original gateway origin and credential.
		current, err := s.Current(ctx)
		if err != nil {
			return nil, err
		}
		c.MediaOutputOrigins = current.Settings.Media.OutputOrigins
	}
	return media.NewConnector(media.Config{Origin: c.Sub2APIOrigin, Keys: c.MediaKeys, ImageCredential: c.ImageCredential, VideoCredential: c.VideoCredential, OutputOrigins: c.MediaOutputOrigins, MaxMediaBytes: c.MaxMediaBytes})
}
func (s *Service) View(ctx context.Context) (View, error) {
	st, err := s.Current(ctx)
	if err != nil {
		return View{}, err
	}
	v := st.Settings
	v.Media.ImageKeyConfigured = v.Media.ImageKey != ""
	v.Media.VideoKeyConfigured = v.Media.VideoKey != ""
	v.Media.ImageKey = ""
	v.Media.VideoKey = ""
	v.Payment.KeyConfigured = v.Payment.Key != ""
	v.Payment.Key = ""
	v.SMTP.PasswordConfigured = v.SMTP.Password != ""
	v.SMTP.Password = ""
	v.Storage.AccessKeyConfigured = v.Storage.AccessKey != ""
	v.Storage.SecretKeyConfigured = v.Storage.SecretKey != ""
	v.Storage.AccessKey = ""
	v.Storage.SecretKey = ""
	err = s.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM assets) OR EXISTS(SELECT 1 FROM runtime_storage_lock)").Scan(&v.Storage.LocationLocked)
	return View{Revision: st.Revision, UpdatedAt: st.UpdatedAt, Settings: v, Readiness: s.Ready(st)}, err
}
func secretFields(v *Settings) map[string]*string {
	return map[string]*string{"media.image_key": &v.Media.ImageKey, "media.video_key": &v.Media.VideoKey, "payment.key": &v.Payment.Key, "smtp.password": &v.SMTP.Password, "storage.access_key": &v.Storage.AccessKey, "storage.secret_key": &v.Storage.SecretKey}
}
func (s *Service) Save(ctx context.Context, actor string, in Update) error {
	in.Settings.Site.Brand = strings.TrimSpace(in.Settings.Site.Brand)
	in.Settings.Site.SupportEmail = strings.TrimSpace(in.Settings.Site.SupportEmail)
	in.Settings.Site.RetentionPolicy = strings.TrimSpace(in.Settings.Site.RetentionPolicy)
	in.Settings.Site.StorageQuotaPolicy = strings.TrimSpace(in.Settings.Site.StorageQuotaPolicy)
	in.Settings.Site.CleanupPolicy = strings.TrimSpace(in.Settings.Site.CleanupPolicy)
	in.Settings.Site.SubmissionRecoveryPolicy = strings.TrimSpace(in.Settings.Site.SubmissionRecoveryPolicy)
	in.Settings.Site.RefundPolicy = strings.TrimSpace(in.Settings.Site.RefundPolicy)
	in.Settings.Media.Origin = strings.TrimRight(strings.TrimSpace(in.Settings.Media.Origin), "/")
	for i, o := range in.Settings.Media.OutputOrigins {
		in.Settings.Media.OutputOrigins[i] = strings.TrimRight(strings.TrimSpace(o), "/")
	}
	in.Settings.Payment.Origin = strings.TrimRight(strings.TrimSpace(in.Settings.Payment.Origin), "/")
	in.Settings.Payment.PID = strings.TrimSpace(in.Settings.Payment.PID)
	in.Settings.SMTP.Host = strings.TrimSpace(in.Settings.SMTP.Host)
	in.Settings.Storage.Endpoint = strings.TrimRight(strings.TrimSpace(in.Settings.Storage.Endpoint), "/")
	if len(strings.TrimSpace(in.Reason)) < 3 || len(in.Reason) > 2000 {
		return FieldErrors{"reason": "请填写 3–2000 字节的变更原因"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(794239106)"); err != nil {
		return err
	}
	// Use this transaction for every read so a single-connection pool cannot deadlock.
	old := s.defaults()
	var raw []byte
	var revision int64
	err = tx.QueryRow(ctx, "SELECT payload,revision FROM runtime_settings WHERE singleton FOR UPDATE").Scan(&raw, &revision)
	if err == nil {
		if err = s.open(raw, "runtime-settings", &old); err != nil {
			return err
		}
		old.Revision = revision
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if in.Revision != old.Revision {
		return ErrConflict
	}
	prev := secretFields(&old.Settings)
	next := secretFields(&in.Settings)
	for name, p := range next {
		if *p == "" {
			*p = *prev[name]
		}
		if strings.Contains(*p, "********") {
			return FieldErrors{name: "请填写新凭证或留空保留原凭证"}
		}
	}
	for _, name := range in.ClearSecrets {
		p, ok := next[name]
		if !ok {
			return FieldErrors{"clear_secrets": "未知凭证字段"}
		}
		*p = ""
	}
	// Read-only display flags are never persisted or used for readiness.
	in.Settings.Media.ImageKeyConfigured = false
	in.Settings.Media.VideoKeyConfigured = false
	in.Settings.Payment.KeyConfigured = false
	in.Settings.SMTP.PasswordConfigured = false
	in.Settings.Storage.AccessKeyConfigured = false
	in.Settings.Storage.SecretKeyConfigured = false
	in.Settings.Storage.LocationLocked = false
	if errs := s.validate(in.Settings); len(errs) > 0 {
		return errs
	}
	var hasAssets bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM assets) OR EXISTS(SELECT 1 FROM runtime_storage_lock) OR EXISTS(SELECT 1 FROM generation_tasks WHERE settlement_status='frozen')").Scan(&hasAssets); err != nil {
		return err
	}
	a, b := old.Settings.Storage, in.Settings.Storage
	if hasAssets && (a.Driver != b.Driver || a.Endpoint != b.Endpoint || a.Region != b.Region || a.Bucket != b.Bucket) {
		return FieldErrors{"storage.bucket": "已有素材，不能直接切换存储位置；请先完成离线迁移"}
	}
	if hasAssets && b.Driver == "s3" && (b.AccessKey == "" || b.SecretKey == "") {
		return FieldErrors{"storage.secret_key": "已有素材，不能清空当前存储凭证；请填写替换凭证"}
	}
	now := time.Now().UTC()
	st := State{Settings: in.Settings, Revision: old.Revision + 1, UpdatedAt: &now, ImageRef: old.ImageRef, VideoRef: old.VideoRef, PaymentRef: old.PaymentRef}
	if !reflect.DeepEqual(old.Settings.Media, in.Settings.Media) {
		st.ImageRef = core.ID("media_image")
		st.VideoRef = core.ID("media_video")
	}
	if !reflect.DeepEqual(old.Settings.Payment, in.Settings.Payment) {
		st.PaymentRef = core.ID("easypay")
	}
	for _, entry := range []struct {
		ref, kind string
		state     State
	}{{old.ImageRef, "media", old}, {old.VideoRef, "media", old}, {old.PaymentRef, "payment", old}, {st.ImageRef, "media", st}, {st.VideoRef, "media", st}, {st.PaymentRef, "payment", st}} {
		if entry.ref == "" {
			continue
		}
		b, e := s.seal(entry.state, "credential:"+entry.kind+":"+entry.ref)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, "INSERT INTO runtime_credential_versions(credential_ref,kind,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", entry.ref, entry.kind, b); err != nil {
			return err
		}
	}
	raw, err = s.seal(st, "runtime-settings")
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO runtime_settings(singleton,revision,payload,updated_by,updated_at) VALUES(true,$1,$2,$3,$4) ON CONFLICT(singleton) DO UPDATE SET revision=EXCLUDED.revision,payload=EXCLUDED.payload,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at", st.Revision, raw, actor, now)
	if err != nil {
		return err
	}
	changed := []string{}
	before, _ := json.Marshal(old.Settings)
	after, _ := json.Marshal(st.Settings)
	var bm, am map[string]map[string]any
	_ = json.Unmarshal(before, &bm)
	_ = json.Unmarshal(after, &am)
	for group, fields := range am {
		for key, value := range fields {
			if !reflect.DeepEqual(value, bm[group][key]) {
				changed = append(changed, group+"."+key)
			}
		}
	}
	sort.Strings(changed)
	details, _ := json.Marshal(map[string]any{"revision": st.Revision, "changed_fields": changed})
	_, err = tx.Exec(ctx, "INSERT INTO admin_audit(id,admin_id,action,target_id,reason,details) VALUES($1,$2,'settings.update','runtime',$3,$4)", core.ID("audit"), actor, in.Reason, details)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) Register(mux *http.ServeMux, admin func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/admin/settings", admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, err := s.View(r.Context())
		if err != nil {
			httpx.Fail(w, 503, "settings_unavailable", "无法读取运行配置，请检查数据库和主密钥")
			return
		}
		httpx.JSON(w, 200, v)
	})))
	mux.Handle("PUT /api/v1/admin/settings", admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in Update
		if !httpx.Decode(w, r, &in) {
			return
		}
		err := s.Save(r.Context(), core.UserFrom(r.Context()).ID, in)
		if errors.Is(err, ErrConflict) {
			httpx.Fail(w, 409, "settings_conflict", "配置已被其他管理员修改，请重新加载后保存")
			return
		}
		var fields FieldErrors
		if errors.As(err, &fields) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(422)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "invalid_settings", "message": "请检查配置字段", "data": map[string]any{"fields": fields}})
			return
		}
		if err != nil {
			httpx.Fail(w, 503, "settings_save_failed", "配置未保存，请检查数据库和主密钥")
			return
		}
		v, err := s.View(r.Context())
		if err != nil {
			httpx.Fail(w, 503, "settings_unavailable", "保存成功但读取失败，请重新加载")
			return
		}
		httpx.JSON(w, 200, v)
	})))
	mux.Handle("POST /api/v1/admin/settings/test/{scope}", admin(http.HandlerFunc(s.test)))
}
func (s *Service) String() string { return fmt.Sprintf("runtime settings (%s)", s.base.Env) }
