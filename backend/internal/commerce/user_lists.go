package commerce

import (
	"net/http"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

func (s *Service) pagedUserTasks(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	f, ok := httpx.QueryFilters(w, r, map[string]int{"q": 200, "project_id": 100, "shot_id": 200, "kind": 20, "status": 60})
	if !ok {
		return
	}
	if f["kind"] != "" && f["kind"] != "image" && f["kind"] != "video" {
		invalid(w, "任务类型无效")
		return
	}
	status := f["status"]
	if status == "all" {
		status = ""
	}
	if !includes([]string{"", "queued", "sending", "submission_unknown", "generating", "completed", "failed", "canceled", "pending", "archiving", "archive_retry", "ready", "error", "frozen", "consumed", "released", "refunded", "disputed", "resolved", "review_required"}, status) {
		invalid(w, "任务状态无效")
		return
	}
	const where = ` WHERE user_id=$1 AND ($2='' OR project_id=$2) AND ($3='' OR kind=$3) AND ($4='' OR execution_status=$4 OR delivery_status=$4 OR settlement_status=$4 OR ($4 IN ('disputed','resolved') AND procurement_status=$4) OR ($4='review_required' AND settlement_status='frozen' AND (execution_status='submission_unknown' OR procurement_status='disputed'))) AND ($5='' OR position(lower($5) IN lower(id||' '||model||' '||COALESCE(input->>'prompt','')))>0) AND ($6='' OR input->>'shot_id'=$6)`
	args := []any{core.UserFrom(r.Context()).ID, f["project_id"], f["kind"], status, f["q"], f["shot_id"]}
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
	items := []Task{}
	for rows.Next() {
		task, e := scanTask(rows)
		if e != nil {
			dbError(w, e)
			return
		}
		items = append(items, task)
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	httpx.JSON(w, 200, page.Result(items, total))
}

func (s *Service) pagedUserOrders(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	f, ok := httpx.QueryFilters(w, r, map[string]int{"q": 200, "out_trade_no": 200, "status": 60})
	if !ok {
		return
	}
	status := f["status"]
	if status == "all" {
		status = ""
	}
	if !includes([]string{"", "pending", "expired", "paid", "credited"}, status) {
		invalid(w, "订单状态无效")
		return
	}
	const where = ` WHERE user_id=$1 AND ($2='' OR position(lower($2) IN lower(id||' '||out_trade_no||' '||COALESCE(trade_no,'')))>0) AND ($3='' OR payment_status=$3 OR credit_status=$3) AND ($4='' OR out_trade_no=$4)`
	args := []any{core.UserFrom(r.Context()).ID, f["q"], status, f["out_trade_no"]}
	var total int64
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM payment_orders"+where, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), "SELECT "+orderColumns+" FROM payment_orders"+where+" ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6", append(args, page.Limit, page.Offset)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []Order{}
	for rows.Next() {
		order, e := scanOrder(rows)
		if e != nil {
			dbError(w, e)
			return
		}
		items = append(items, order)
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	httpx.JSON(w, 200, page.Result(items, total))
}

func (s *Service) pagedUserLedger(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	f, ok := httpx.QueryFilters(w, r, map[string]int{"q": 200, "event_type": 60})
	if !ok {
		return
	}
	if !includes([]string{"", "adjustment", "freeze", "consumed", "released", "refund", "recharge"}, f["event_type"]) {
		invalid(w, "流水类型无效")
		return
	}
	const where = ` WHERE user_id=$1 AND ($2='' OR position(lower($2) IN lower(id||' '||reason||' '||COALESCE(task_id,'')||' '||COALESCE(order_id,'')))>0) AND ($3='' OR event_type=$3)`
	args := []any{core.UserFrom(r.Context()).ID, f["q"], f["event_type"]}
	var total int64
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM wallet_ledger"+where, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), "SELECT id,event_type,available_delta,frozen_delta,available_after,frozen_after,COALESCE(task_id,''),COALESCE(order_id,''),reason,created_at FROM wallet_ledger"+where+" ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5", append(args, page.Limit, page.Offset)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []Ledger{}
	for rows.Next() {
		var l Ledger
		if err = rows.Scan(&l.ID, &l.EventType, &l.AvailableDelta, &l.FrozenDelta, &l.AvailableAfter, &l.FrozenAfter, &l.TaskID, &l.OrderID, &l.Reason, &l.CreatedAt); err != nil {
			dbError(w, err)
			return
		}
		items = append(items, l)
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	httpx.JSON(w, 200, page.Result(items, total))
}
