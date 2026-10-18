// Package operations exposes read-only health and capacity facts for operators.
package operations

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/httpx"
)

type Service struct {
	db          *pgxpool.Pool
	reviewAfter time.Duration
}

func New(db *pgxpool.Pool, cfg config.Config) *Service {
	after := cfg.TaskReviewAfter
	if after <= 0 {
		after = 24 * time.Hour
	}
	return &Service{db: db, reviewAfter: after}
}
func (s *Service) Register(mux *http.ServeMux, admin func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/admin/overview", admin(http.HandlerFunc(s.overview)))
	mux.Handle("GET /api/v1/admin/operations/signals", admin(http.HandlerFunc(s.signals)))
}

// Counts share one PostgreSQL statement snapshot. This endpoint only reports
// facts: it cannot clean objects, cancel jobs, release funds, or credit orders.
func (s *Service) overview(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRow(r.Context(), `SELECT jsonb_build_object(
	'generated_at',now(),
	'users',(SELECT jsonb_build_object('total',count(*),'active',count(*) FILTER(WHERE status='active'),'disabled',count(*) FILTER(WHERE status='disabled'),'unverified',count(*) FILTER(WHERE NOT email_verified)) FROM users),
	'projects',(SELECT jsonb_build_object('active',count(*) FILTER(WHERE status='active' AND deleted_at IS NULL),'archived',count(*) FILTER(WHERE status='archived' AND deleted_at IS NULL),'deleted',count(*) FILTER(WHERE deleted_at IS NOT NULL)) FROM projects),
	'assets',(SELECT jsonb_build_object('active_count',count(*) FILTER(WHERE deleted_at IS NULL),'active_bytes',coalesce(sum(size_bytes) FILTER(WHERE deleted_at IS NULL),0),'retained_count',count(*),'retained_bytes',coalesce(sum(size_bytes),0),'capacity_basis','logical_database_records') FROM assets),
	'tasks',(SELECT jsonb_build_object('review_required',count(*) FILTER(WHERE settlement_status='frozen' AND (execution_status='submission_unknown' OR procurement_status='disputed')),'frozen_overdue',count(*) FILTER(WHERE settlement_status='frozen' AND created_at<$1),'archive_failed',count(*) FILTER(WHERE settlement_status='frozen' AND execution_status='completed' AND (delivery_status IN ('archive_retry','error') OR error_code='archive_review')),'frozen_count',count(*) FILTER(WHERE settlement_status='frozen')) FROM generation_tasks),
	'wallet',(SELECT jsonb_build_object('available_total',coalesce(sum(available),0),'frozen_total',coalesce(sum(frozen),0)) FROM wallet_accounts),
	'payments',(SELECT jsonb_build_object('pending_count',count(*) FILTER(WHERE credit_status='pending'),'review_required',count(*) FILTER(WHERE credit_status='pending' AND (payment_status='paid' OR expires_at<now() OR check_attempts>=3))) FROM payment_orders),
	'procurement_balance',jsonb_build_object('status','unavailable','reason','Sub2API 尚无经核实的服务账户余额公开接口')
	)`, time.Now().Add(-s.reviewAfter)).Scan(&raw)
	if err != nil {
		httpx.Fail(w, 503, "overview_unavailable", "暂时无法读取运营概览")
		return
	}
	httpx.JSON(w, 200, raw)
}
