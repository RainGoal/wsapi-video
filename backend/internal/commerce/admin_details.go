package commerce

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"wsapi-video/internal/httpx"
)

type adminTaskRecord struct {
	Task
	UserID        string `json:"user_id"`
	UpstreamID    string `json:"upstream_id"`
	CredentialRef string `json:"credential_ref"`
	Attempts      int    `json:"attempts"`
}

func adminTaskRecordOf(t Task) adminTaskRecord {
	return adminTaskRecord{Task: t, UserID: t.UserID, UpstreamID: t.UpstreamID, CredentialRef: t.CredentialRef, Attempts: t.Attempts}
}

type adminOrderRecord struct {
	Order
	UserID         string `json:"user_id"`
	MerchantPID    string `json:"merchant_pid"`
	ProviderOrigin string `json:"provider_origin"`
}

func adminOrderRecordOf(o Order) adminOrderRecord {
	return adminOrderRecord{Order: o, UserID: o.UserID, MerchantPID: o.MerchantPID, ProviderOrigin: o.ProviderOrigin}
}

func (s *Service) registerAdminDetails(mux *http.ServeMux, admin func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/admin/tasks/{id}", admin(http.HandlerFunc(s.adminTaskDetail)))
	mux.Handle("GET /api/v1/admin/payment/orders/{id}", admin(http.HandlerFunc(s.adminOrderDetail)))
}

func (s *Service) adminTaskDetail(w http.ResponseWriter, r *http.Request) {
	t, err := s.getTask(r.Context(), "", r.PathValue("id"), true)
	if err != nil {
		dbError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), "SELECT id,attempt,action,status,created_at,completed_at FROM task_attempts WHERE task_id=$1 ORDER BY attempt DESC", t.ID)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	type attempt struct {
		ID          string     `json:"id"`
		Attempt     int        `json:"attempt"`
		Action      string     `json:"action"`
		Status      string     `json:"status"`
		CreatedAt   time.Time  `json:"created_at"`
		CompletedAt *time.Time `json:"completed_at"`
	}
	history := []attempt{}
	for rows.Next() {
		var a attempt
		if err = rows.Scan(&a.ID, &a.Attempt, &a.Action, &a.Status, &a.CreatedAt, &a.CompletedAt); err != nil {
			dbError(w, err)
			return
		}
		history = append(history, a)
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	httpx.JSON(w, 200, struct {
		adminTaskRecord
		AttemptHistory []attempt `json:"attempt_history"`
	}{adminTaskRecordOf(t), history})
}

func (s *Service) adminOrderDetail(w http.ResponseWriter, r *http.Request) {
	o, err := s.getOrder(r.Context(), "", r.PathValue("id"), true)
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 200, adminOrderRecordOf(o))
}

func (s *Service) pagedAdminTasks(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	if len(search) > 200 || len(q.Get("status")) > 60 || len(q.Get("user_id")) > 100 || len(q.Get("project_id")) > 100 || (q.Get("kind") != "" && q.Get("kind") != "image" && q.Get("kind") != "video") {
		invalid(w, "搜索或筛选参数无效")
		return
	}
	reviewAfter := s.cfg.TaskReviewAfter
	if reviewAfter <= 0 {
		reviewAfter = 24 * time.Hour
	}
	const where = ` WHERE ($1='' OR position(lower($1) IN lower(id||' '||model||' '||COALESCE(upstream_id,'')))>0 OR EXISTS(SELECT 1 FROM users u WHERE u.id=generation_tasks.user_id AND position(lower($1) IN lower(u.email||' '||u.display_name))>0)) AND ($2='' OR execution_status=$2 OR delivery_status=$2 OR settlement_status=$2 OR procurement_status=$2 OR ($2='review_required' AND settlement_status='frozen' AND (execution_status='submission_unknown' OR procurement_status='disputed')) OR ($2='frozen_overdue' AND settlement_status='frozen' AND created_at<$6) OR ($2='lease_expired' AND settlement_status='frozen' AND lease_until<now()) OR ($2='archive_failed' AND settlement_status='frozen' AND execution_status='completed' AND (delivery_status IN ('archive_retry','error') OR error_code='archive_review')) OR ($2='procurement_review_disputed' AND (SELECT p.status FROM task_procurement_reviews p WHERE p.task_id=generation_tasks.id ORDER BY p.revision DESC LIMIT 1)='disputed')) AND ($3='' OR kind=$3) AND ($4='' OR user_id=$4) AND ($5='' OR project_id=$5)`
	args := []any{search, q.Get("status"), q.Get("kind"), q.Get("user_id"), q.Get("project_id"), time.Now().Add(-reviewAfter)}
	var total int64
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM generation_tasks"+where, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), "SELECT "+taskColumns+" FROM generation_tasks"+where+" ORDER BY created_at DESC,id DESC LIMIT $7 OFFSET $8", append(args, page.Limit, page.Offset)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	out := []adminTaskRecord{}
	for rows.Next() {
		task, e := scanTask(rows)
		if e != nil {
			dbError(w, e)
			return
		}
		out = append(out, adminTaskRecordOf(task))
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	httpx.JSON(w, 200, page.Result(out, total))
}

func (s *Service) pagedAdminOrders(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	if len(search) > 200 || len(q.Get("status")) > 60 || len(q.Get("user_id")) > 100 {
		invalid(w, "搜索或筛选参数无效")
		return
	}
	const where = ` WHERE ($1='' OR position(lower($1) IN lower(id||' '||out_trade_no||' '||COALESCE(trade_no,'')))>0 OR EXISTS(SELECT 1 FROM users u WHERE u.id=payment_orders.user_id AND position(lower($1) IN lower(u.email||' '||u.display_name))>0)) AND ($2='' OR payment_status=$2 OR credit_status=$2 OR ($2='review_required' AND credit_status='pending' AND (payment_status='paid' OR expires_at<now() OR check_attempts>=3))) AND ($3='' OR user_id=$3)`
	args := []any{search, q.Get("status"), q.Get("user_id")}
	var total int64
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM payment_orders"+where, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), "SELECT "+orderColumns+" FROM payment_orders"+where+" ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5", append(args, page.Limit, page.Offset)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	out := []adminOrderRecord{}
	for rows.Next() {
		order, e := scanOrder(rows)
		if e != nil {
			dbError(w, e)
			return
		}
		out = append(out, adminOrderRecordOf(order))
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	httpx.JSON(w, 200, page.Result(out, total))
}

func (s *Service) pagedAuditList(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	if len(search) > 200 || len(q.Get("action")) > 100 {
		invalid(w, "搜索或筛选参数无效")
		return
	}
	const source = `(SELECT id,admin_id,action,target_id,reason,details,created_at FROM admin_audit UNION ALL SELECT id,actor_id,'user.'||action,target_user_id,reason,jsonb_build_object('previous_status',previous_status,'next_status',next_status),created_at FROM identity_admin_audit) a`
	const where = ` WHERE ($1='' OR position(lower($1) IN lower(a.id||' '||a.admin_id||' '||a.action||' '||a.target_id||' '||a.reason))>0) AND ($2='' OR a.action=$2)`
	var total int64
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM "+source+where, search, q.Get("action")).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), "SELECT a.id,a.admin_id,a.action,a.target_id,a.reason,a.details,a.created_at FROM "+source+where+" ORDER BY a.created_at DESC,a.id DESC LIMIT $3 OFFSET $4", search, q.Get("action"), page.Limit, page.Offset)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, admin, action, target, reason string
		var details json.RawMessage
		var created time.Time
		if err = rows.Scan(&id, &admin, &action, &target, &reason, &details, &created); err != nil {
			dbError(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "admin_id": admin, "action": action, "target_id": target, "reason": reason, "details": details, "created_at": created})
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	httpx.JSON(w, 200, page.Result(out, total))
}
