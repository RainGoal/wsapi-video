package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/media"
)

func (s *Service) adminProducts(w http.ResponseWriter, r *http.Request) {
	out, err := s.listProducts(r.Context(), true)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}
func validateProduct(p Product) bool {
	c := p.Capabilities
	if c.MaxReferenceDurationSeconds < 0 || c.MaxReferenceDurationSeconds > 86400 || c.MaxReferenceBytes < 0 || c.MaxReferenceBytes > 1<<40 || c.MaxReferenceWidth < 0 || c.MaxReferenceWidth > 32768 || c.MaxReferenceHeight < 0 || c.MaxReferenceHeight > 32768 {
		return false
	}
	if p.ID == "" || len(p.ID) > 100 || p.Name == "" || len(p.Name) > 200 || p.Model == "" || len(p.Model) > 200 || p.Credits < 1 || p.Credits > 1000000000 || (p.Kind != "image" && p.Kind != "video") {
		return false
	}
	if c.MaxImages < 0 || c.MaxImages > 16 || c.MaxVideos < 0 || c.MaxVideos > 8 || c.MaxAudios < 0 || c.MaxAudios > 8 || len(c.Durations) > 64 || len(c.Resolutions) > 32 || len(c.Sizes) > 32 || len(c.Qualities) > 16 || len(c.AspectRatios) > 32 {
		return false
	}
	for _, d := range c.Durations {
		if d < 1 || d > 600 {
			return false
		}
	}
	for _, ls := range [][]string{c.Resolutions, c.Sizes, c.Qualities, c.AspectRatios} {
		for _, v := range ls {
			if strings.TrimSpace(v) == "" || len(v) > 100 {
				return false
			}
		}
	}
	if p.Kind == "image" && (len(c.Durations) > 0 || len(c.Resolutions) > 0 || c.SupportsFrames || c.SupportsAudio || c.SupportsCancel || c.MaxVideos > 0 || c.MaxAudios > 0) {
		return false
	}
	if p.Kind == "video" && (len(c.Sizes) > 0 || len(c.Qualities) > 0) {
		return false
	}
	return true
}

func sameProductCapabilities(a, b Capabilities) bool {
	normalize := func(c Capabilities) Capabilities {
		for _, values := range []*[]string{&c.AspectRatios, &c.Resolutions, &c.Sizes, &c.Qualities} {
			if len(*values) == 0 {
				*values = nil
			}
		}
		if len(c.Durations) == 0 {
			c.Durations = nil
		}
		return c
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}

// Publishing validates the configured credential for this model kind. Storage
// readiness still gates user listings, quotes and actual generation separately.
func (s *Service) productCredential(ctx context.Context, kind string) (string, error) {
	if s.settings == nil {
		return s.gateway.Credential(kind)
	}
	c, err := s.settings.RuntimeConfig(ctx)
	if err != nil {
		return "", err
	}
	g, err := media.NewConnector(media.Config{Origin: c.Sub2APIOrigin, Keys: c.MediaKeys, ImageCredential: c.ImageCredential, VideoCredential: c.VideoCredential, OutputOrigins: c.MediaOutputOrigins, MaxMediaBytes: c.MaxMediaBytes})
	if err != nil {
		return "", err
	}
	return g.Credential(kind)
}

func (s *Service) saveProduct(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Product
		Reason string `json:"reason"`
	}
	if !httpx.Decode(w, r, &body) || !requireReason(w, body.Reason) {
		return
	}
	p := body.Product
	p.ID = r.PathValue("id")
	if !validateProduct(p) {
		invalid(w, "商品名称、积分或能力配置无效")
		return
	}
	if p.Demo && (!s.isDemo() || p.Kind != "image") {
		invalid(w, "仅开发演示模式允许上架图片演示商品")
		return
	}
	// Runtime settings may use the same pool with a single connection. Resolve
	// credentials before opening a transaction, and enforce the result only if
	// the final product is still being published after definition comparison.
	var credentialErr error
	if p.Active && !p.Demo {
		_, credentialErr = s.productCredential(r.Context(), p.Kind)
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	old, err := scanProduct(tx.QueryRow(ctx, "SELECT "+productColumns+" FROM model_products WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", p.ID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		dbError(w, err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var deleted bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM model_products WHERE id=$1 AND deleted_at IS NOT NULL)", p.ID).Scan(&deleted); e != nil {
			dbError(w, e)
			return
		}
		if deleted {
			conflict(w, "该模型商品已删除，不能复用其 ID")
			return
		}
	}
	if err == nil && old.Version != p.Version {
		conflict(w, "商品已更新，请重新加载")
		return
	}
	if err == nil && old.Demo != p.Demo {
		invalid(w, "已有商品的演示属性不可更改")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) && p.Version > 1 {
		conflict(w, "新商品版本应为 0 或 1")
		return
	}
	if err == nil && (old.Kind != p.Kind || old.Model != p.Model || !sameProductCapabilities(old.Capabilities, p.Capabilities)) {
		// Verification covers the saved model definition. Changes must first be
		// saved offline; a later versioned request can confirm the new definition.
		p.Verified, p.Active = false, false
	}
	if p.Active && !p.Verified {
		invalid(w, "上架前必须确认商品能力已验证")
		return
	}
	if p.Active && !p.Demo && credentialErr != nil {
		httpx.Fail(w, 503, "gateway_unavailable", "请先配置对应生成凭证，再上架真实商品")
		return
	}
	p.Version = old.Version + 1
	c, _ := json.Marshal(p.Capabilities)
	// A missing row cannot be locked by SELECT FOR UPDATE. Check the observed
	// version again in the write so a concurrent creation cannot be overwritten.
	tag, err := tx.Exec(ctx, "INSERT INTO model_products(id,name,kind,model,credits,version,capabilities,active,verified,is_demo) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET name=excluded.name,kind=excluded.kind,model=excluded.model,credits=excluded.credits,version=excluded.version,capabilities=excluded.capabilities,active=excluded.active,verified=excluded.verified,updated_at=now() WHERE model_products.version=$11 AND model_products.deleted_at IS NULL", p.ID, p.Name, p.Kind, p.Model, p.Credits, p.Version, c, p.Active, p.Verified, p.Demo, old.Version)
	if err == nil && tag.RowsAffected() == 0 {
		conflict(w, "商品已更新或删除，请重新加载；已删除 ID 不可复用")
		return
	}
	if err == nil {
		err = audit(ctx, tx, core.UserFrom(ctx).ID, "product.save", p.ID, body.Reason, map[string]any{"before": old, "after": p})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, p)
}
func (s *Service) adminTiers(w http.ResponseWriter, r *http.Request) {
	out, err := s.listTiers(r.Context(), true)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

func (s *Service) deleteProduct(w http.ResponseWriter, r *http.Request) {
	s.deleteCatalog(w, r, "model_products", "product.delete")
}

func (s *Service) deleteTier(w http.ResponseWriter, r *http.Request) {
	s.deleteCatalog(w, r, "payment_tiers", "payment_tier.delete")
}

// table and action are fixed by the handlers above, never request parameters.
func (s *Service) deleteCatalog(w http.ResponseWriter, r *http.Request, table, action string) {
	var body struct {
		Version int64  `json:"version"`
		Reason  string `json:"reason"`
	}
	if !httpx.Decode(w, r, &body) || !requireReason(w, body.Reason) {
		return
	}
	if body.Version < 1 {
		invalid(w, "请提供当前版本后再删除")
		return
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	id := r.PathValue("id")
	var version int64
	err = tx.QueryRow(ctx, "SELECT version FROM "+table+" WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", id).Scan(&version)
	if err != nil {
		dbError(w, err)
		return
	}
	if version != body.Version {
		conflict(w, "目录项已更新，请重新加载后删除")
		return
	}
	_, err = tx.Exec(ctx, "UPDATE "+table+" SET deleted_at=now(),active=false,version=version+1,updated_at=now() WHERE id=$1", id)
	if err == nil {
		err = audit(ctx, tx, core.UserFrom(ctx).ID, action, id, body.Reason, map[string]any{
			"before": map[string]any{"version": version},
			"after":  map[string]any{"version": version + 1, "active": false, "deleted": true},
		})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "version": version + 1})
}

func (s *Service) saveTier(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tier
		Reason string `json:"reason"`
	}
	if !httpx.Decode(w, r, &body) || !requireReason(w, body.Reason) {
		return
	}
	t := body.Tier
	t.ID = r.PathValue("id")
	if len(t.ID) < 1 || len(t.ID) > 100 || len(t.Name) < 1 || len(t.Name) > 100 || t.AmountFen < 1 || t.AmountFen > 100000000 || t.Credits < 1 || t.Credits > 1000000000 {
		invalid(w, "充值档位参数无效")
		return
	}
	if t.Active {
		if _, err := s.currentPayment(); err != nil {
			httpx.Fail(w, 503, "payment_unavailable", "请先配置并验证真实支付渠道")
			return
		}
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var old Tier
	err = tx.QueryRow(ctx, "SELECT id,name,amount_fen,credits,active,version FROM payment_tiers WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", t.ID).Scan(&old.ID, &old.Name, &old.AmountFen, &old.Credits, &old.Active, &old.Version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		dbError(w, err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var deleted bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM payment_tiers WHERE id=$1 AND deleted_at IS NOT NULL)", t.ID).Scan(&deleted); e != nil {
			dbError(w, e)
			return
		}
		if deleted {
			conflict(w, "该充值档位已删除，不能复用其 ID")
			return
		}
	}
	if err == nil && old.Version != t.Version {
		conflict(w, "档位已更新，请重新加载")
		return
	}
	t.Version = old.Version + 1
	tag, err := tx.Exec(ctx, "INSERT INTO payment_tiers(id,name,amount_fen,credits,active,version) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET name=excluded.name,amount_fen=excluded.amount_fen,credits=excluded.credits,active=excluded.active,version=excluded.version,updated_at=now() WHERE payment_tiers.version=$7 AND payment_tiers.deleted_at IS NULL", t.ID, t.Name, t.AmountFen, t.Credits, t.Active, t.Version, old.Version)
	if err == nil && tag.RowsAffected() == 0 {
		conflict(w, "档位已更新或删除，请重新加载；已删除 ID 不可复用")
		return
	}
	if err == nil {
		err = audit(ctx, tx, core.UserFrom(ctx).ID, "payment_tier.save", t.ID, body.Reason, map[string]any{"before": old, "after": t})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, t)
}
func (s *Service) adminTasks(w http.ResponseWriter, r *http.Request) {
	s.pagedAdminTasks(w, r)
}
func (s *Service) resolveTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action     string `json:"action"`
		UpstreamID string `json:"upstream_id"`
		Evidence   string `json:"evidence"`
		Reason     string `json:"reason"`
	}
	if !httpx.Decode(w, r, &body) || !requireReason(w, body.Reason) {
		return
	}
	ctx := r.Context()
	t, err := s.getTask(ctx, "", r.PathValue("id"), true)
	if err != nil {
		dbError(w, err)
		return
	}
	if t.SettlementStatus != "frozen" {
		conflict(w, "仅冻结中的任务可作异常决议")
		return
	}
	var verified media.Result
	if body.Action == "bind_upstream" {
		if t.ExecutionStatus != "submission_unknown" || len(strings.TrimSpace(body.Evidence)) < 10 || len(body.Evidence) > 4000 {
			invalid(w, "仅待确认提交可绑定原任务，须记录可追溯核查证据")
			return
		}
		verified, err = s.gateway.Poll(ctx, t.CredentialRef, t.Kind, body.UpstreamID)
		if err != nil || verified.ID != body.UpstreamID || (verified.Model != "" && verified.Model != t.Model) {
			invalid(w, "原服务凭证无法核实此网关任务")
			return
		}
	}
	if body.Action != "release" && body.Action != "bind_upstream" && body.Action != "retry_archive" {
		invalid(w, "未知的异常处理动作")
		return
	}
	if body.Action == "retry_archive" && t.ExecutionStatus != "completed" {
		invalid(w, "仅已完成生成可重试保存")
		return
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var state, execution string
	var lease *time.Time
	err = tx.QueryRow(ctx, "SELECT settlement_status,execution_status,lease_until FROM generation_tasks WHERE id=$1 FOR UPDATE", t.ID).Scan(&state, &execution, &lease)
	if err != nil {
		dbError(w, err)
		return
	}
	if state != "frozen" || execution != t.ExecutionStatus {
		conflict(w, "任务状态已改变，请重新加载")
		return
	}
	switch body.Action {
	case "release":
		if lease != nil && lease.After(time.Now()) && t.ExecutionStatus != "submission_unknown" && t.ProcurementStatus != "disputed" {
			conflict(w, "Worker 正在处理此任务，请等待当前执行结束")
			return
		}
		if err = changeWallet(ctx, tx, t.UserID, t.Credits, -t.Credits, "task:"+t.ID+":released", "released", t.ID, "", body.Reason); err == nil {
			_, err = tx.Exec(ctx, "UPDATE generation_tasks SET settlement_status='released',procurement_status='resolved',execution_status=CASE WHEN execution_status='completed' THEN execution_status ELSE 'failed' END,delivery_status=CASE WHEN delivery_status='ready' THEN delivery_status ELSE 'error' END,error_code='operator_released',error_message='已由运营释放预冻结积分',lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=$1", t.ID)
		}
	case "bind_upstream":
		execution, delivery, settlement := "generating", "pending", "frozen"
		code, message := "", ""
		outputs := t.Outputs
		switch verified.State {
		case "completed":
			// Preserve the terminal result already checked with the original
			// credential. Old tasks must not re-enter the overdue polling gate.
			execution, outputs = "completed", verified.Outputs
		case "failed", "canceled":
			execution, delivery, settlement = verified.State, "error", "released"
			code, message = "generation_failed", "生成未完成，预冻结积分已释放"
			err = changeWallet(ctx, tx, t.UserID, t.Credits, -t.Credits, "task:"+t.ID+":released", "released", t.ID, "", body.Reason)
		}
		if err == nil {
			raw, _ := json.Marshal(outputs)
			_, err = tx.Exec(ctx, "UPDATE generation_tasks SET upstream_id=$2,execution_status=$3,delivery_status=$4,settlement_status=$5,outputs=$6,procurement_status='resolved',error_code=$7,error_message=$8,lease_token=NULL,lease_until=NULL,next_attempt_at=now(),updated_at=now() WHERE id=$1", t.ID, body.UpstreamID, execution, delivery, settlement, raw, code, message)
		}
	case "retry_archive":
		if lease != nil && lease.After(time.Now()) && t.ProcurementStatus != "disputed" {
			conflict(w, "当前作品正在保存，请稍后重试")
			return
		}
		_, err = tx.Exec(ctx, "UPDATE generation_tasks SET delivery_status='archive_retry',error_code='',error_message='',procurement_status='resolved',lease_token=NULL,lease_until=NULL,next_attempt_at=now(),updated_at=now() WHERE id=$1", t.ID)
	}
	if err == nil {
		err = audit(ctx, tx, core.UserFrom(ctx).ID, "task."+body.Action, t.ID, body.Reason, map[string]any{"previous": t, "upstream_id": body.UpstreamID, "evidence": body.Evidence})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	out, err := s.getTask(ctx, "", t.ID, true)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, out)
}
func (s *Service) refundTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !httpx.Decode(w, r, &body) || !requireReason(w, body.Reason) {
		return
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM generation_tasks WHERE id=$1 FOR UPDATE", r.PathValue("id")))
	if err != nil {
		dbError(w, err)
		return
	}
	if t.SettlementStatus == "refunded" {
		httpx.JSON(w, 200, t)
		return
	}
	if t.SettlementStatus != "consumed" {
		conflict(w, "只有已消费任务可退积分，冻结中任务请先核查释放")
		return
	}
	err = changeWallet(ctx, tx, t.UserID, t.Credits, 0, "task:"+t.ID+":refund", "refund", t.ID, "", body.Reason)
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE generation_tasks SET settlement_status='refunded',procurement_status='resolved',updated_at=now() WHERE id=$1", t.ID)
	}
	if err == nil {
		err = audit(ctx, tx, core.UserFrom(ctx).ID, "task.refund", t.ID, body.Reason, map[string]any{"credits": t.Credits})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	out, err := s.getTask(ctx, "", t.ID, true)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, out)
}
func (s *Service) adjustWallet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID     string `json:"user_id"`
		Amount     int64  `json:"amount"`
		RequestKey string `json:"request_key"`
		Reason     string `json:"reason"`
	}
	if !httpx.Decode(w, r, &body) || !requireReason(w, body.Reason) {
		return
	}
	if body.UserID == "" || body.Amount == 0 || body.Amount > 1000000000 || body.Amount < -1000000000 || len(body.RequestKey) < 8 || len(body.RequestKey) > 128 {
		invalid(w, "调账用户、积分或去重键无效")
		return
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var available, frozen int64
	err = tx.QueryRow(ctx, "SELECT available,frozen FROM wallet_accounts WHERE user_id=$1 FOR UPDATE", body.UserID).Scan(&available, &frozen)
	if err != nil {
		dbError(w, err)
		return
	}
	key := "adjust:" + body.UserID + ":" + body.RequestKey
	var oldAmount int64
	var oldReason string
	err = tx.QueryRow(ctx, "SELECT available_delta,reason FROM wallet_ledger WHERE event_key=$1", key).Scan(&oldAmount, &oldReason)
	if err == nil {
		if oldAmount != body.Amount || oldReason != body.Reason {
			conflict(w, "调账去重键已用于其他金额或原因")
			return
		}
		httpx.JSON(w, 200, Wallet{Available: available, Frozen: frozen, Demo: s.isDemo()})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		dbError(w, err)
		return
	}
	err = changeWallet(ctx, tx, body.UserID, body.Amount, 0, key, "adjustment", "", "", body.Reason)
	if errors.Is(err, errFunds) {
		conflict(w, "余额不足以执行此调账")
		return
	}
	if err == nil {
		err = audit(ctx, tx, core.UserFrom(ctx).ID, "wallet.adjust", body.UserID, body.Reason, map[string]any{"amount": body.Amount, "request_key": body.RequestKey, "demo": s.isDemo()})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, Wallet{Available: available + body.Amount, Frozen: frozen, Demo: s.isDemo()})
}
func (s *Service) adminOrders(w http.ResponseWriter, r *http.Request) {
	s.pagedAdminOrders(w, r)
}
func (s *Service) adminCheckOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !httpx.Decode(w, r, &body) || !requireReason(w, body.Reason) {
		return
	}
	ctx := r.Context()
	o, err := s.getOrder(ctx, "", r.PathValue("id"), true)
	if err != nil {
		dbError(w, err)
		return
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	// Persist the administrator's intent before a reconciliation may credit money.
	// A later audit outage must never leave a successful manual credit untraceable.
	err = audit(ctx, tx, core.UserFrom(ctx).ID, "payment.check_requested", o.ID, body.Reason, map[string]any{"out_trade_no": o.OutTradeNo})
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if err = s.confirmOrder(ctx, o, nil); err != nil {
		httpx.Fail(w, 502, "payment_query_failed", "支付渠道查询失败，未执行人工到账")
		return
	}
	out, err := s.getOrder(ctx, "", o.ID, true)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, out)
}
func (s *Service) auditList(w http.ResponseWriter, r *http.Request) {
	s.pagedAuditList(w, r)
}
