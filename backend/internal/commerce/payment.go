package commerce

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/observability"
	"wsapi-video/internal/payments"
)

const orderColumns = "id,user_id,out_trade_no,amount_fen,credits,method,payment_status,credit_status,checkout_url,created_at,expires_at,paid_at,credited_at,credential_ref,merchant_pid,provider_origin"

func scanOrder(row scanner) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.UserID, &o.OutTradeNo, &o.AmountFen, &o.Credits, &o.Method, &o.PaymentStatus, &o.CreditStatus, &o.CheckoutURL, &o.CreatedAt, &o.ExpiresAt, &o.PaidAt, &o.CreditedAt, &o.CredentialRef, &o.MerchantPID, &o.ProviderOrigin)
	return o, err
}
func (s *Service) getOrder(ctx context.Context, userID, id string, admin bool) (Order, error) {
	return scanOrder(s.db.QueryRow(ctx, "SELECT "+orderColumns+" FROM payment_orders WHERE id=$1 AND ($3 OR user_id=$2)", id, userID, admin))
}
func (s *Service) listTiers(ctx context.Context, all bool) ([]Tier, error) {
	rows, err := s.db.Query(ctx, "SELECT id,name,amount_fen,credits,active,version FROM payment_tiers WHERE $1 OR active ORDER BY amount_fen", all)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tier{}
	for rows.Next() {
		var t Tier
		if err = rows.Scan(&t.ID, &t.Name, &t.AmountFen, &t.Credits, &t.Active, &t.Version); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Service) tiers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.currentPayment(); err != nil {
		httpx.JSON(w, 200, map[string]any{"items": []Tier{}, "available": false, "demo": s.isDemo()})
		return
	}
	out, err := s.listTiers(r.Context(), false)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "available": true, "demo": false})
}
func (s *Service) createOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TierID string `json:"tier_id"`
		Method string `json:"method"`
	}
	if !httpx.Decode(w, r, &body) {
		return
	}
	if body.Method != "alipay" && body.Method != "wxpay" {
		invalid(w, "请选择支付宝或微信支付")
		return
	}
	p, paymentConfig, err := s.currentPaymentConfig(r.Context())
	if err != nil {
		httpx.Fail(w, 503, "payment_unavailable", "充值服务尚未开放，演示模式不支持真实支付")
		return
	}
	var tier Tier
	err = s.db.QueryRow(r.Context(), "SELECT id,name,amount_fen,credits,active,version FROM payment_tiers WHERE id=$1 AND active", body.TierID).Scan(&tier.ID, &tier.Name, &tier.AmountFen, &tier.Credits, &tier.Active, &tier.Version)
	if err != nil {
		dbError(w, err)
		return
	}
	id := core.ID("order")
	no := "CV" + strings.ReplaceAll(core.ID(""), "_", "")
	checkout, err := p.Checkout(no, body.Method, tier.AmountFen)
	if err != nil {
		invalid(w, "支付参数无效")
		return
	}
	_, err = s.db.Exec(r.Context(), "INSERT INTO payment_orders(id,user_id,out_trade_no,tier_id,tier_version,amount_fen,credits,method,credential_ref,merchant_pid,provider_origin,checkout_url,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", id, core.UserFrom(r.Context()).ID, no, tier.ID, tier.Version, tier.AmountFen, tier.Credits, body.Method, paymentConfig.EasyPayCredentialRef, paymentConfig.EasyPayPID, strings.TrimRight(paymentConfig.EasyPayOrigin, "/"), checkout, time.Now().Add(30*time.Minute))
	if err != nil {
		dbError(w, err)
		return
	}
	observability.Event(r.Context(), slog.LevelInfo, "payment_order_created", "accepted", observability.Correlation{OrderID: id})
	o, err := s.getOrder(r.Context(), core.UserFrom(r.Context()).ID, id, false)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 201, o)
}
func (s *Service) listOrders(ctx context.Context, userID string, admin bool) ([]Order, error) {
	rows, err := s.db.Query(ctx, "SELECT "+orderColumns+" FROM payment_orders WHERE $1 OR user_id=$2 ORDER BY created_at DESC LIMIT 200", admin, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, e := scanOrder(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Service) orders(w http.ResponseWriter, r *http.Request) {
	s.pagedUserOrders(w, r)
}
func (s *Service) order(w http.ResponseWriter, r *http.Request) {
	o, err := s.getOrder(r.Context(), core.UserFrom(r.Context()).ID, r.PathValue("id"), false)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, o)
}
func (s *Service) checkOrder(w http.ResponseWriter, r *http.Request) {
	o, err := s.getOrder(r.Context(), core.UserFrom(r.Context()).ID, r.PathValue("id"), false)
	if err != nil {
		dbError(w, err)
		return
	}
	if err = s.confirmOrder(r.Context(), o, nil); err != nil {
		httpx.Fail(w, 502, "payment_query_failed", "支付渠道暂不可查询，请稍后重试")
		return
	}
	o, err = s.getOrder(r.Context(), o.UserID, o.ID, false)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, o)
}

func (s *Service) confirmOrder(ctx context.Context, o Order, notification *payments.Confirmation) (err error) {
	failureCode := "adapter_unavailable"
	defer func() {
		if err != nil {
			observability.Event(ctx, slog.LevelWarn, "payment_confirmation_failed", failureCode, observability.Correlation{OrderID: o.ID})
		}
	}()
	if o.CreditStatus == "credited" {
		return nil
	}
	p, err := s.paymentAdapterContext(ctx, o.CredentialRef, o.ProviderOrigin, o.MerchantPID)
	if err != nil {
		return err
	}
	failureCode = "payment_query_failed"
	var result payments.Confirmation
	if notification != nil {
		result, err = p.Confirm(ctx, *notification)
	} else {
		result, err = p.Query(ctx, o.OutTradeNo)
	}
	if err != nil {
		return err
	}
	if result.OrderNo != o.OutTradeNo || result.AmountFen != o.AmountFen {
		failureCode = "payment_mismatch"
		return errors.New("payment order or amount mismatch")
	}
	failureCode = "database_error"
	if !result.Paid {
		_, err = s.db.Exec(ctx, "UPDATE payment_orders SET payment_status=CASE WHEN expires_at<now() THEN 'expired' ELSE payment_status END,next_check_at=now()+interval '5 minutes',check_attempts=check_attempts+1,lease_until=NULL WHERE id=$1 AND credit_status='pending'", o.ID)
		return err
	}
	if result.TradeNo == "" {
		failureCode = "payment_trade_missing"
		return errors.New("paid order has no provider trade number")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, "SELECT credit_status FROM payment_orders WHERE id=$1 FOR UPDATE", o.ID).Scan(&status)
	if err != nil {
		return err
	}
	if status == "credited" {
		return nil
	}
	if err = changeWallet(ctx, tx, o.UserID, o.Credits, 0, "order:"+o.ID+":credit", "recharge", "", o.ID, "易支付充值到账"); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE payment_orders SET payment_status='paid',credit_status='credited',trade_no=$2,paid_at=now(),credited_at=now(),lease_until=NULL WHERE id=$1", o.ID, result.TradeNo)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	observability.Event(ctx, slog.LevelInfo, "payment_credited", "accepted", observability.Correlation{OrderID: o.ID})
	return nil
}
func (s *Service) notify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	values := r.URL.Query()
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			observability.Event(ctx, slog.LevelWarn, "payment_notification_rejected", "invalid_input", observability.Correlation{})
			http.Error(w, "fail", 400)
			return
		}
		post, err := url.ParseQuery(string(b))
		if err != nil {
			observability.Event(ctx, slog.LevelWarn, "payment_notification_rejected", "invalid_input", observability.Correlation{})
			http.Error(w, "fail", 400)
			return
		}
		for k, vs := range post {
			values[k] = append(values[k], vs...)
		}
	}
	if len(values["out_trade_no"]) != 1 {
		observability.Event(ctx, slog.LevelWarn, "payment_notification_rejected", "invalid_input", observability.Correlation{})
		http.Error(w, "fail", 400)
		return
	}
	o, err := scanOrder(s.db.QueryRow(ctx, "SELECT "+orderColumns+" FROM payment_orders WHERE out_trade_no=$1", values.Get("out_trade_no")))
	if err != nil {
		observability.Event(ctx, slog.LevelWarn, "payment_notification_rejected", "invalid_input", observability.Correlation{})
		http.Error(w, "fail", 400)
		return
	}
	p, err := s.paymentAdapterContext(ctx, o.CredentialRef, o.ProviderOrigin, o.MerchantPID)
	if err != nil {
		observability.Event(ctx, slog.LevelWarn, "payment_notification_rejected", "adapter_unavailable", observability.Correlation{OrderID: o.ID})
		http.Error(w, "fail", 503)
		return
	}
	n, err := p.Verify(values)
	if err != nil || !n.Paid || n.OrderNo != o.OutTradeNo || n.AmountFen != o.AmountFen {
		observability.Event(ctx, slog.LevelWarn, "payment_notification_rejected", "invalid_signature", observability.Correlation{OrderID: o.ID})
		http.Error(w, "fail", 400)
		return
	}
	if err = s.confirmOrder(ctx, o, &n); err != nil {
		http.Error(w, "fail", 503)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "success")
}

func (s *Service) reconcilePayments(ctx context.Context) {
	// Reconcile late paid orders for 7 days; older pending orders remain visible for manual lookup.
	rows, err := s.db.Query(ctx, "UPDATE payment_orders SET lease_until=now()+interval '2 minutes' WHERE id IN (SELECT id FROM payment_orders WHERE credit_status='pending' AND next_check_at<=now() AND created_at>now()-interval '7 days' AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_check_at FOR UPDATE SKIP LOCKED LIMIT 10) RETURNING "+orderColumns)
	if err != nil {
		observability.Event(ctx, slog.LevelWarn, "payment_reconcile_failed", "database_error", observability.Correlation{})
		return
	}
	out := []Order{}
	for rows.Next() {
		o, e := scanOrder(rows)
		if e == nil {
			out = append(out, o)
		} else {
			observability.Event(ctx, slog.LevelWarn, "payment_reconcile_failed", "order_scan_failed", observability.Correlation{})
		}
	}
	if rows.Err() != nil {
		observability.Event(ctx, slog.LevelWarn, "payment_reconcile_failed", "database_error", observability.Correlation{})
	}
	rows.Close()
	for _, o := range out {
		if ctx.Err() != nil {
			return
		}
		if err = s.confirmOrder(ctx, o, nil); err != nil {
			_, _ = s.db.Exec(ctx, "UPDATE payment_orders SET next_check_at=now()+interval '5 minutes',check_attempts=check_attempts+1,lease_until=NULL WHERE id=$1 AND credit_status='pending'", o.ID)
		}
	}
}
