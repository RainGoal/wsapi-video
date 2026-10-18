// Package commerce owns retail quotes, the immutable wallet ledger, reliable
// media jobs and independent recharge orders. PostgreSQL is the source of truth.
package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/media"
	"wsapi-video/internal/observability"
	"wsapi-video/internal/payments"
	"wsapi-video/internal/settings"
)

type AssetService interface {
	Resolve(context.Context, string, string) (assets.Asset, error)
	ReferenceURL(context.Context, string, string) (string, error)
	Archived(context.Context, string, string, string) (assets.Asset, error)
	Archive(context.Context, string, string, string, string, string, io.Reader) (assets.Asset, error)
}
type Service struct {
	db       *pgxpool.Pool
	cfg      config.Config
	assets   AssetService
	gateway  gatewayClient
	settings *settings.Service
}

type gatewayClient interface {
	Credential(string) (string, error)
	Submit(context.Context, string, media.Input) (media.Result, error)
	Poll(context.Context, string, string, string) (media.Result, error)
	Cancel(context.Context, string, string) error
	OpenVideo(context.Context, string, string) (io.ReadCloser, string, error)
	OpenImage(context.Context, media.Output) (io.ReadCloser, string, error)
}

func (s *Service) SetRuntimeSettings(v *settings.Service) {
	s.settings = v
	s.gateway = &settings.GatewayRouter{Settings: v}
}

func New(db *pgxpool.Pool, cfg config.Config, a AssetService) *Service {
	g, _ := media.NewConnector(media.Config{Origin: cfg.Sub2APIOrigin, Keys: cfg.MediaKeys, ImageCredential: cfg.ImageCredential, VideoCredential: cfg.VideoCredential, OutputOrigins: cfg.MediaOutputOrigins, MaxMediaBytes: cfg.MaxMediaBytes})
	return &Service{db: db, cfg: cfg, assets: a, gateway: g}
}
func (s *Service) Register(mux *http.ServeMux, protect func(http.Handler) http.Handler, admin func(http.Handler) http.Handler) {
	user := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, protect(h)) }
	user("GET /api/v1/model-products", s.products)
	user("POST /api/v1/quotes", s.quote)
	user("POST /api/v1/tasks", s.submit)
	user("GET /api/v1/tasks", s.tasks)
	user("GET /api/v1/tasks/{id}", s.task)
	user("POST /api/v1/tasks/{id}/cancel", s.cancel)
	user("GET /api/v1/wallet", s.wallet)
	user("GET /api/v1/wallet/ledger", s.ledger)
	user("GET /api/v1/payment/tiers", s.tiers)
	user("POST /api/v1/payment/orders", s.createOrder)
	user("GET /api/v1/payment/orders", s.orders)
	user("GET /api/v1/payment/orders/{id}", s.order)
	user("POST /api/v1/payment/orders/{id}/check", s.checkOrder)
	mux.HandleFunc("GET /api/v1/payment/notify/easypay", s.notify)
	mux.HandleFunc("POST /api/v1/payment/notify/easypay", s.notify)
	a := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, admin(h)) }
	a("GET /api/v1/admin/model-products", s.adminProducts)
	a("PUT /api/v1/admin/model-products/{id}", s.saveProduct)
	a("DELETE /api/v1/admin/model-products/{id}", s.deleteProduct)
	a("GET /api/v1/admin/payment/tiers", s.adminTiers)
	a("PUT /api/v1/admin/payment/tiers/{id}", s.saveTier)
	a("DELETE /api/v1/admin/payment/tiers/{id}", s.deleteTier)
	a("GET /api/v1/admin/tasks", s.adminTasks)
	a("GET /api/v1/admin/tasks/{id}/procurement-reviews", s.procurementReviews)
	a("POST /api/v1/admin/tasks/{id}/procurement-reviews", s.saveProcurementReview)
	a("POST /api/v1/admin/tasks/{id}/resolve", s.resolveTask)
	a("POST /api/v1/admin/tasks/{id}/refund", s.refundTask)
	a("GET /api/v1/admin/payment/orders", s.adminOrders)
	a("POST /api/v1/admin/payment/orders/{id}/check", s.adminCheckOrder)
	a("POST /api/v1/admin/wallet/adjust", s.adjustWallet)
	a("GET /api/v1/admin/audit", s.auditList)
	s.registerAdminDetails(mux, admin)
}
func dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, 404, "not_found", "记录不存在")
		return
	}
	httpx.Fail(w, 500, "database_error", "操作暂时失败，请重试")
}
func invalid(w http.ResponseWriter, message string)  { httpx.Fail(w, 422, "invalid_input", message) }
func conflict(w http.ResponseWriter, message string) { httpx.Fail(w, 409, "conflict", message) }
func generationUnavailable(w http.ResponseWriter, err error) {
	if errors.Is(err, settings.ErrStorageUnavailable) {
		httpx.Fail(w, 503, "storage_unavailable", "私有存储尚未配置，请管理员在系统设置中完成私有存储配置后重试")
		return
	}
	httpx.Fail(w, 503, "gateway_unavailable", "生成服务尚未配置")
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func unique(err error) bool     { var p *pgconn.PgError; return errors.As(err, &p) && p.Code == "23505" }
func (s *Service) isDemo() bool { return s.cfg.DevMode && s.cfg.Demo }
func (s *Service) ownProject(ctx context.Context, userID, projectID string) error {
	var id string
	return s.db.QueryRow(ctx, "SELECT id FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL AND status='active'", projectID, userID).Scan(&id)
}

const productColumns = "id,name,kind,model,credits,version,capabilities,active,verified,is_demo,pricing"

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (Product, error) {
	var p Product
	var raw, pricing []byte
	err := row.Scan(&p.ID, &p.Name, &p.Kind, &p.Model, &p.Credits, &p.Version, &raw, &p.Active, &p.Verified, &p.Demo, &pricing)
	if err == nil {
		err = json.Unmarshal(raw, &p.Capabilities)
	}
	if err == nil && len(pricing) > 0 {
		err = json.Unmarshal(pricing, &p.Pricing)
	}
	return p, err
}
func (s *Service) listProducts(ctx context.Context, all bool) ([]Product, error) {
	var current gatewayClient = s.gateway
	if !all && s.settings != nil && !s.isDemo() {
		g, err := s.settings.Gateway(ctx, "")
		if errors.Is(err, settings.ErrStorageUnavailable) {
			return nil, err
		}
		if errors.Is(err, media.ErrUnavailable) {
			return []Product{}, nil
		}
		if err != nil {
			return nil, err
		}
		current = g
	}
	rows, err := s.db.Query(ctx, "SELECT "+productColumns+" FROM model_products WHERE deleted_at IS NULL AND ($1 OR (active AND verified AND is_demo=$2)) ORDER BY kind,name", all, s.isDemo())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Product{}
	for rows.Next() {
		p, e := scanProduct(rows)
		if e != nil {
			return nil, e
		}
		if !all && !p.Demo {
			if _, e = current.Credential(p.Kind); e != nil {
				continue
			}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Service) products(w http.ResponseWriter, r *http.Request) {
	p, err := s.listProducts(r.Context(), false)
	if errors.Is(err, settings.ErrStorageUnavailable) {
		generationUnavailable(w, err)
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": p, "demo": s.isDemo()})
}

func includes[T comparable](items []T, v T) bool {
	for _, item := range items {
		if item == v {
			return true
		}
	}
	return false
}
func validateVideoSpecifications(in QuoteInput, p Product) error {
	if p.Kind != "video" {
		return nil
	}
	if in.Size != "" || in.Quality != "" {
		return errors.New("视频生成不支持图片尺寸或质量参数，请使用视频分辨率")
	}
	if len(p.Capabilities.Sizes) > 0 || len(p.Capabilities.Qualities) > 0 {
		return errors.New("此视频商品包含未支持的图片规格，请管理员修正后重新报价")
	}
	return nil
}

func validateInput(in *QuoteInput, p Product) error {
	if err := validateVideoSpecifications(*in, p); err != nil {
		return err
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.ProjectID == "" || in.ProductID == "" || in.Prompt == "" || len(in.Prompt) > 12000 {
		return errors.New("项目、商品与提示词不能为空；提示词最多 12000 字节")
	}
	if len(in.ShotID) > 200 {
		return errors.New("镜头标识过长")
	}
	c := p.Capabilities
	if in.AspectRatio == "" && len(c.AspectRatios) > 0 {
		in.AspectRatio = c.AspectRatios[0]
	}
	if in.Duration == 0 && len(c.Durations) > 0 {
		in.Duration = c.Durations[0]
	}
	if in.Resolution == "" && len(c.Resolutions) > 0 {
		in.Resolution = c.Resolutions[0]
	}
	if p.Kind == "video" && in.Resolution != "" {
		for _, resolution := range c.Resolutions {
			if resolutionKey(in.Resolution) == resolutionKey(resolution) {
				in.Resolution = resolution
				break
			}
		}
	}
	if in.Size == "" && len(c.Sizes) > 0 {
		in.Size = c.Sizes[0]
	}
	if in.Quality == "" && len(c.Qualities) > 0 {
		in.Quality = c.Qualities[0]
	}
	if (in.AspectRatio != "" && !includes(c.AspectRatios, in.AspectRatio)) || (in.Duration != 0 && !includes(c.Durations, in.Duration)) || (in.Resolution != "" && !includes(c.Resolutions, in.Resolution)) || (in.Size != "" && !includes(c.Sizes, in.Size)) || (in.Quality != "" && !includes(c.Qualities, in.Quality)) {
		return errors.New("生成参数不属于已验证的商品规格")
	}
	if in.Audio != nil && !c.SupportsAudio {
		return errors.New("此商品不支持音频控制")
	}
	if (in.StartFrameAssetID != "" || in.EndFrameAssetID != "") && !c.SupportsFrames {
		return errors.New("此商品不支持首尾帧")
	}
	if len(in.ReferenceAssetIDs) > 32 {
		return errors.New("参考素材过多")
	}
	seen := map[string]bool{}
	for _, id := range inputAssets(*in) {
		if strings.TrimSpace(id) == "" || len(id) > 200 || seen[id] {
			return errors.New("参考素材标识为空或重复")
		}
		seen[id] = true
	}
	if p.Kind == "image" && (in.Duration != 0 || in.Resolution != "" || in.Audio != nil || in.StartFrameAssetID != "" || in.EndFrameAssetID != "") {
		return errors.New("图片商品包含不支持的视频参数")
	}
	return nil
}
func inputAssets(in QuoteInput) []string {
	out := append([]string{}, in.ReferenceAssetIDs...)
	if in.StartFrameAssetID != "" {
		out = append(out, in.StartFrameAssetID)
	}
	if in.EndFrameAssetID != "" {
		out = append(out, in.EndFrameAssetID)
	}
	return out
}
func (s *Service) validateAssets(ctx context.Context, userID string, in QuoteInput, p Product) error {
	n := map[string]int{}
	seen := map[string]bool{}
	var totalBytes int64
	var totalSeconds float64
	c := p.Capabilities
	for _, id := range inputAssets(in) {
		if seen[id] {
			continue
		}
		seen[id] = true
		a, err := s.assets.Resolve(ctx, userID, id)
		if err != nil {
			return errors.New("参考素材不存在或无访问权限")
		}
		if (id == in.StartFrameAssetID || id == in.EndFrameAssetID) && a.Kind != "image" {
			return errors.New("首尾帧必须使用图片")
		}
		if includes(in.ReferenceAssetIDs, id) {
			n[a.Kind]++
		}
		if a.SizeBytes < 0 || a.SizeBytes > 1<<40 {
			return errors.New("参考素材大小元数据无效")
		}
		totalBytes += a.SizeBytes
		if a.Kind == "video" || a.Kind == "audio" {
			if a.DurationSeconds <= 0 || math.IsNaN(a.DurationSeconds) || math.IsInf(a.DurationSeconds, 0) {
				return errors.New("参考媒体缺少可靠时长，不能报价")
			}
			totalSeconds += a.DurationSeconds
		}
		if (c.MaxReferenceWidth > 0 && (a.Kind == "image" || a.Kind == "video") && (a.Width <= 0 || a.Width > c.MaxReferenceWidth)) || (c.MaxReferenceHeight > 0 && (a.Kind == "image" || a.Kind == "video") && (a.Height <= 0 || a.Height > c.MaxReferenceHeight)) {
			return errors.New("参考素材尺寸超出商品限制")
		}
	}
	if n["image"] > c.MaxImages || n["video"] > c.MaxVideos || n["audio"] > c.MaxAudios {
		return errors.New("参考素材数量或类型超出商品限制")
	}
	if (c.MaxReferenceBytes > 0 && totalBytes > c.MaxReferenceBytes) || (c.MaxReferenceDurationSeconds > 0 && totalSeconds > float64(c.MaxReferenceDurationSeconds)) {
		return errors.New("参考素材总大小或总时长超出商品限制")
	}
	return nil
}
func (s *Service) quote(w http.ResponseWriter, r *http.Request) {
	var in QuoteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	u := core.UserFrom(r.Context())
	if err := s.ownProject(r.Context(), u.ID, in.ProjectID); err != nil {
		dbError(w, err)
		return
	}
	p, err := scanProduct(s.db.QueryRow(r.Context(), "SELECT "+productColumns+" FROM model_products WHERE id=$1 AND deleted_at IS NULL AND active AND verified AND is_demo=$2", in.ProductID, s.isDemo()))
	if err != nil {
		dbError(w, err)
		return
	}
	if !p.Demo {
		if _, err = s.gateway.Credential(p.Kind); err != nil {
			generationUnavailable(w, err)
			return
		}
	}
	if err = validateInput(&in, p); err != nil {
		invalid(w, err.Error())
		return
	}
	if err = s.validateAssets(r.Context(), u.ID, in, p); err != nil {
		invalid(w, err.Error())
		return
	}
	if in.ShotID != "" {
		var owned bool
		err = s.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM shots WHERE id=$1 AND project_id=$2 AND user_id=$3)", in.ShotID, in.ProjectID, u.ID).Scan(&owned)
		if err != nil {
			dbError(w, err)
			return
		}
		if !owned {
			invalid(w, "镜头不存在或不属于当前项目，请先保存分镜")
			return
		}
	}
	credits, pricing, err := calculateProductPrice(p, in)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	q := Quote{ID: core.ID("quote"), ProductID: p.ID, Credits: credits, Pricing: pricing, InputHash: digest(in), ExpiresAt: time.Now().UTC().Add(5 * time.Minute), Demo: p.Demo}
	ib, _ := json.Marshal(in)
	pb, _ := json.Marshal(p)
	_, err = s.db.Exec(r.Context(), "INSERT INTO quotes(id,user_id,project_id,product_id,input,input_hash,product_snapshot,credits,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", q.ID, u.ID, in.ProjectID, p.ID, ib, q.InputHash, pb, q.Credits, q.ExpiresAt)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 201, q)
}

// changeWallet must be called inside the same transaction as the business state.
// The unique immutable event and row lock make retries and concurrent workers safe.
func changeWallet(ctx context.Context, tx pgx.Tx, userID string, availableDelta, frozenDelta int64, eventKey, eventType, taskID, orderID, reason string) error {
	var available, frozen int64
	if err := tx.QueryRow(ctx, "SELECT available,frozen FROM wallet_accounts WHERE user_id=$1 FOR UPDATE", userID).Scan(&available, &frozen); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM wallet_ledger WHERE event_key=$1)", eventKey).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if available+availableDelta < 0 || frozen+frozenDelta < 0 {
		return errFunds
	}
	if available+availableDelta > 9000000000000000 || frozen+frozenDelta > 9000000000000000 {
		return errors.New("wallet capacity exceeded")
	}
	_, err := tx.Exec(ctx, "UPDATE wallet_accounts SET available=available+$2,frozen=frozen+$3,updated_at=now() WHERE user_id=$1", userID, availableDelta, frozenDelta)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO wallet_ledger(id,user_id,event_key,event_type,available_delta,frozen_delta,available_after,frozen_after,task_id,order_id,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),$11)", core.ID("ledger"), userID, eventKey, eventType, availableDelta, frozenDelta, available+availableDelta, frozen+frozenDelta, taskID, orderID, reason)
	return err
}

var errFunds = errors.New("insufficient available credits")

const taskColumns = "id,user_id,project_id,product_id,kind,model,credits,execution_status,delivery_status,settlement_status,procurement_status,asset_ids,error_code,error_message,is_demo,created_at,updated_at,input,product_snapshot,credential_ref,COALESCE(upstream_id,''),COALESCE(lease_token,''),attempts,outputs"

func scanTask(row scanner) (Task, error) {
	var t Task
	var a, in, p, out []byte
	err := row.Scan(&t.ID, &t.UserID, &t.ProjectID, &t.ProductID, &t.Kind, &t.Model, &t.Credits, &t.ExecutionStatus, &t.DeliveryStatus, &t.SettlementStatus, &t.ProcurementStatus, &a, &t.ErrorCode, &t.ErrorMessage, &t.Demo, &t.CreatedAt, &t.UpdatedAt, &in, &p, &t.CredentialRef, &t.UpstreamID, &t.LeaseToken, &t.Attempts, &out)
	if err != nil {
		return t, err
	}
	for _, x := range []struct {
		b      []byte
		target any
	}{{a, &t.AssetIDs}, {in, &t.Input}, {p, &t.Product}, {out, &t.Outputs}} {
		if err = json.Unmarshal(x.b, x.target); err != nil {
			return t, err
		}
	}
	t.Prompt = t.Input.Prompt
	return t, nil
}
func (s *Service) getTask(ctx context.Context, userID, id string, admin bool) (Task, error) {
	return scanTask(s.db.QueryRow(ctx, "SELECT "+taskColumns+" FROM generation_tasks WHERE id=$1 AND ($3 OR user_id=$2)", id, userID, admin))
}
func (s *Service) submit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		QuoteID    string `json:"quote_id"`
		RequestKey string `json:"request_key"`
	}
	if !httpx.Decode(w, r, &body) {
		return
	}
	if body.QuoteID == "" || len(body.RequestKey) < 8 || len(body.RequestKey) > 128 {
		invalid(w, "报价不能为空，提交去重键长度须为 8–128")
		return
	}
	ctx := r.Context()
	u := core.UserFrom(ctx)
	hash := digest(body.QuoteID)
	// Accepted requests win over later quote expiry or product changes.
	var originalID, originalHash string
	err := s.db.QueryRow(ctx, "SELECT id,request_hash FROM generation_tasks WHERE user_id=$1 AND request_key=$2", u.ID, body.RequestKey).Scan(&originalID, &originalHash)
	if err == nil {
		if originalHash != hash {
			conflict(w, "该提交键已用于其他报价")
			return
		}
		t, e := s.getTask(ctx, u.ID, originalID, false)
		if e != nil {
			dbError(w, e)
			return
		}
		httpx.JSON(w, 200, t)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		dbError(w, err)
		return
	}
	// Resolve the active connector before acquiring a transaction. Runtime
	// settings use the same pool, which may intentionally have only one slot.
	var activeGateway gatewayClient = s.gateway
	var activeGatewayErr error
	if s.settings != nil {
		g, e := s.settings.Gateway(ctx, "")
		if e != nil {
			activeGateway = nil
			activeGatewayErr = e
		} else {
			activeGateway = g
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var projectID, productID string
	var inRaw, pRaw []byte
	var credits int64
	var expires time.Time
	err = tx.QueryRow(ctx, "SELECT project_id,product_id,input,product_snapshot,credits,expires_at FROM quotes WHERE id=$1 AND user_id=$2 FOR UPDATE", body.QuoteID, u.ID).Scan(&projectID, &productID, &inRaw, &pRaw, &credits, &expires)
	if err != nil {
		dbError(w, err)
		return
	}
	// The quote row serializes all retries including different request keys.
	var acceptedID, acceptedKey string
	err = tx.QueryRow(ctx, "SELECT id,request_key FROM generation_tasks WHERE quote_id=$1", body.QuoteID).Scan(&acceptedID, &acceptedKey)
	if err == nil {
		if acceptedKey != body.RequestKey {
			conflict(w, "该报价已经创建任务，请获取新报价")
			return
		}
		t, e := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM generation_tasks WHERE id=$1 AND user_id=$2", acceptedID, u.ID))
		if e != nil {
			dbError(w, e)
			return
		}
		httpx.JSON(w, 200, t)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		dbError(w, err)
		return
	}
	if time.Now().After(expires) {
		httpx.Fail(w, 409, "quote_expired", "报价已过期，请重新确认费用")
		return
	}
	// Deleting a catalog entry stops new acceptance while preserving accepted
	// task retries above and their immutable product snapshots. The shared lock
	// orders this acceptance before or after a concurrent catalog deletion.
	var availableProduct string
	err = tx.QueryRow(ctx, "SELECT id FROM model_products WHERE id=$1 AND deleted_at IS NULL FOR SHARE", productID).Scan(&availableProduct)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, 409, "product_unavailable", "该模型商品已删除，请重新选择商品并报价")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	var p Product
	var in QuoteInput
	if json.Unmarshal(pRaw, &p) != nil || json.Unmarshal(inRaw, &in) != nil {
		dbError(w, errors.New("invalid snapshot"))
		return
	}
	if err = validateVideoSpecifications(in, p); err != nil {
		invalid(w, err.Error())
		return
	}
	quotedCredits, _, err := calculateProductPrice(p, in)
	if err != nil || quotedCredits != credits {
		invalid(w, "报价价格与冻结的商品规格不一致，请重新报价")
		return
	}
	if p.Demo != s.isDemo() {
		httpx.Fail(w, 503, "demo_unavailable", "演示模式已关闭")
		return
	}
	ref := "demo"
	if !p.Demo {
		if activeGateway == nil {
			err = activeGatewayErr
			if err == nil {
				err = media.ErrUnavailable
			}
		} else {
			ref, err = activeGateway.Credential(p.Kind)
		}
		if err != nil {
			generationUnavailable(w, err)
			return
		}
	}
	var owned string
	err = tx.QueryRow(ctx, "SELECT id FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL AND status='active' FOR SHARE", projectID, u.ID).Scan(&owned)
	if err != nil {
		dbError(w, err)
		return
	}
	if in.ShotID != "" {
		var shot string
		err = tx.QueryRow(ctx, "SELECT id FROM shots WHERE id=$1 AND project_id=$2 AND user_id=$3 FOR SHARE", in.ShotID, projectID, u.ID).Scan(&shot)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				invalid(w, "报价关联的镜头已删除，请保存分镜并重新报价")
			} else {
				dbError(w, err)
			}
			return
		}
	}
	id := core.ID("task")
	_, err = tx.Exec(ctx, "INSERT INTO generation_tasks(id,user_id,project_id,quote_id,product_id,request_key,request_hash,input,product_snapshot,credits,kind,model,credential_ref,is_demo) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)", id, u.ID, projectID, body.QuoteID, productID, body.RequestKey, hash, inRaw, pRaw, credits, p.Kind, p.Model, ref, p.Demo)
	if unique(err) {
		conflict(w, "并发提交已受理，请使用相同提交键重试")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	for _, assetID := range inputAssets(in) {
		var asset string
		err = tx.QueryRow(ctx, "SELECT id FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR SHARE", assetID, u.ID).Scan(&asset)
		if err != nil {
			invalid(w, "参考素材已不可用，请重新报价")
			return
		}
		_, err = tx.Exec(ctx, "INSERT INTO asset_references(asset_id,project_id,owner_type,owner_id) VALUES($1,$2,'task',$3) ON CONFLICT DO NOTHING", assetID, projectID, id)
		if err != nil {
			dbError(w, err)
			return
		}
	}
	if err = changeWallet(ctx, tx, u.ID, -credits, credits, "task:"+id+":freeze", "freeze", id, "", "生成任务预冻结"); errors.Is(err, errFunds) {
		httpx.Fail(w, 402, "insufficient_credits", "可用积分不足")
		return
	} else if err != nil {
		dbError(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		dbError(w, err)
		return
	}
	observability.Event(ctx, slog.LevelInfo, "task_created", "frozen", observability.Correlation{TaskID: id})
	t, err := s.getTask(ctx, u.ID, id, false)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 201, t)
}
func (s *Service) listTasks(ctx context.Context, userID, projectID, kind, status string, admin bool) ([]Task, error) {
	rows, err := s.db.Query(ctx, "SELECT "+taskColumns+" FROM generation_tasks WHERE ($1 OR user_id=$2) AND ($3='' OR project_id=$3) AND ($4='' OR kind=$4) AND ($5='' OR execution_status=$5 OR delivery_status=$5) ORDER BY created_at DESC LIMIT 200", admin, userID, projectID, kind, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Service) tasks(w http.ResponseWriter, r *http.Request) {
	s.pagedUserTasks(w, r)
}
func (s *Service) task(w http.ResponseWriter, r *http.Request) {
	t, err := s.getTask(r.Context(), core.UserFrom(r.Context()).ID, r.PathValue("id"), false)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, t)
}
func (s *Service) wallet(w http.ResponseWriter, r *http.Request) {
	var out Wallet
	out.Demo = s.isDemo()
	err := s.db.QueryRow(r.Context(), "SELECT available,frozen FROM wallet_accounts WHERE user_id=$1", core.UserFrom(r.Context()).ID).Scan(&out.Available, &out.Frozen)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, out)
}
func (s *Service) ledger(w http.ResponseWriter, r *http.Request) {
	s.pagedUserLedger(w, r)
}

func (s *Service) settle(ctx context.Context, id, leaseToken, settlement, execution, delivery, code, message string) (err error) {
	defer func() {
		if err != nil {
			observability.Event(ctx, slog.LevelWarn, "task_settlement_failed", "database_error", observability.Correlation{TaskID: id})
		}
	}()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID, current string
	var credits int64
	err = tx.QueryRow(ctx, "SELECT user_id,credits,settlement_status FROM generation_tasks WHERE id=$1 AND COALESCE(lease_token,'')=$2 FOR UPDATE", id, leaseToken).Scan(&userID, &credits, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} // An expired worker cannot settle a new lease.
	if err != nil {
		return err
	}
	if current != "frozen" {
		return nil
	}
	available := int64(0)
	if settlement == "released" {
		available = credits
	}
	if err = changeWallet(ctx, tx, userID, available, -credits, "task:"+id+":"+settlement, settlement, id, "", message); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE generation_tasks SET execution_status=$2,delivery_status=$3,settlement_status=$4,error_code=$5,error_message=$6,lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=$1", id, execution, delivery, settlement, code, message)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	observability.Event(ctx, slog.LevelInfo, "task_settlement", settlement, observability.Correlation{TaskID: id})
	return nil
}
func (s *Service) paymentAdapter(ref, origin, pid string) (*payments.EasyPay, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.paymentAdapterContext(ctx, ref, origin, pid)
}
func (s *Service) paymentAdapterContext(ctx context.Context, ref, origin, pid string) (*payments.EasyPay, error) {
	c := s.cfg
	if s.settings != nil {
		var err error
		c, err = s.settings.CredentialConfig(ctx, ref, "payment")
		if err != nil {
			return nil, err
		}
		if c.EasyPayOrigin != origin || c.EasyPayPID != pid {
			return nil, errors.New("original payment merchant does not match order")
		}
	}
	key := c.EasyPayKeys[ref]
	if ref == c.EasyPayCredentialRef && key == "" {
		key = c.EasyPayKey
	}
	return payments.New(payments.Config{Origin: origin, PID: pid, Key: key, NotifyURL: c.EasyPayNotifyURL, ReturnURL: c.EasyPayReturnURL, CredentialRef: ref, AllowHTTP: c.DevMode})
}
func (s *Service) currentPayment() (*payments.EasyPay, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, _, err := s.currentPaymentConfig(ctx)
	return p, err
}
func (s *Service) currentPaymentConfig(ctx context.Context) (*payments.EasyPay, config.Config, error) {
	if s.isDemo() {
		return nil, config.Config{}, errors.New("real payment is disabled in demo mode")
	}
	c := s.cfg
	if s.settings != nil {
		st, err := s.settings.Current(ctx)
		if err != nil {
			return nil, c, err
		}
		if !s.settings.Ready(st).Payment {
			return nil, c, errors.New("payment is disabled or not configured")
		}
		c = s.settings.Config(st)
	}
	key := c.EasyPayKeys[c.EasyPayCredentialRef]
	if key == "" {
		key = c.EasyPayKey
	}
	p, err := payments.New(payments.Config{Origin: c.EasyPayOrigin, PID: c.EasyPayPID, Key: key, NotifyURL: c.EasyPayNotifyURL, ReturnURL: c.EasyPayReturnURL, CredentialRef: c.EasyPayCredentialRef, AllowHTTP: c.DevMode})
	return p, c, err
}
func audit(ctx context.Context, tx pgx.Tx, adminID, action, targetID, reason string, details any) error {
	raw, _ := json.Marshal(details)
	_, err := tx.Exec(ctx, "INSERT INTO admin_audit(id,admin_id,action,target_id,reason,details) VALUES($1,$2,$3,$4,$5,$6)", core.ID("audit"), adminID, action, targetID, reason, raw)
	return err
}
func reasonValid(reason string) bool {
	return len(strings.TrimSpace(reason)) >= 3 && len(reason) <= 2000
}
func requireReason(w http.ResponseWriter, reason string) bool {
	if !reasonValid(reason) {
		invalid(w, "操作原因须为 3–2000 字节")
		return false
	}
	return true
}
