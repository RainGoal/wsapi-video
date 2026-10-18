package commerce

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
)

type ProcurementReview struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Revision  int64  `json:"revision"`
	AdminID   string `json:"admin_id"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	Evidence  string `json:"evidence"`
	CreatedAt string `json:"created_at"`
}

const procurementReviewFields = `id,task_id,revision,admin_id,status,reason,evidence,to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS created_at`

func scanProcurementReview(row pgx.Row) (ProcurementReview, error) {
	var review ProcurementReview
	err := row.Scan(&review.ID, &review.TaskID, &review.Revision, &review.AdminID, &review.Status, &review.Reason, &review.Evidence, &review.CreatedAt)
	return review, err
}

func (s *Service) procurementReviews(w http.ResponseWriter, r *http.Request) {
	page, ok := httpx.Pagination(w, r)
	if !ok {
		return
	}
	var exists bool
	var total int64
	var currentRaw, itemsRaw []byte
	err := s.db.QueryRow(r.Context(), procurementReviewsSQL, r.PathValue("id"), page.Limit, page.Offset).Scan(&exists, &total, &currentRaw, &itemsRaw)
	if err != nil {
		dbError(w, err)
		return
	}
	if !exists {
		dbError(w, pgx.ErrNoRows)
		return
	}
	var current ProcurementReview
	var items []ProcurementReview
	if err = json.Unmarshal(currentRaw, &current); err == nil {
		err = json.Unmarshal(itemsRaw, &items)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	result := page.Result(items, total)
	result["current"] = current
	httpx.JSON(w, 200, result)
}

func (s *Service) saveProcurementReview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExpectedRevision *int64 `json:"expected_revision"`
		Status           string `json:"status"`
		Reason           string `json:"reason"`
		Evidence         string `json:"evidence"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	in.Evidence = strings.TrimSpace(in.Evidence)
	if in.ExpectedRevision == nil || *in.ExpectedRevision < 0 || len(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 2000 || utf8.RuneCountInString(in.Evidence) > 2000 {
		invalid(w, "请提供当前核对版本及操作原因；原因至少 3 字节，原因与证据各最多 2000 个字符")
		return
	}
	if in.Status != "pending" && in.Status != "matched" && in.Status != "disputed" && in.Status != "resolved" {
		invalid(w, "采购核对状态无效")
		return
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var taskID string
	// This serializes review revisions without updating any task fields. Existing
	// procurement_status belongs to worker exception handling and stays untouched.
	err = tx.QueryRow(ctx, "SELECT id FROM generation_tasks WHERE id=$1 FOR UPDATE", r.PathValue("id")).Scan(&taskID)
	if err != nil {
		dbError(w, err)
		return
	}
	previous, err := scanProcurementReview(tx.QueryRow(ctx, "SELECT "+procurementReviewFields+" FROM task_procurement_reviews WHERE task_id=$1 ORDER BY revision DESC LIMIT 1", taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		previous, err = ProcurementReview{Status: "pending"}, nil
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if previous.Revision != *in.ExpectedRevision || previous.Revision == math.MaxInt64 {
		conflict(w, "采购核对记录已更新，请重新加载后再提交")
		return
	}
	review, err := scanProcurementReview(tx.QueryRow(ctx, `INSERT INTO task_procurement_reviews(id,task_id,revision,admin_id,status,reason,evidence)
 VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+procurementReviewFields, core.ID("review"), taskID, previous.Revision+1, core.UserFrom(ctx).ID, in.Status, in.Reason, in.Evidence))
	if err == nil {
		err = audit(ctx, tx, core.UserFrom(ctx).ID, "task.procurement_review", taskID, in.Reason, map[string]any{"before": previous, "after": review})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	httpx.JSON(w, 201, review)
}

// The current record, count and history page use one statement snapshot.
const procurementReviewsSQL = `WITH reviews AS (
 SELECT ` + procurementReviewFields + ` FROM task_procurement_reviews WHERE task_id=$1
), page AS (
 SELECT * FROM reviews ORDER BY revision DESC LIMIT $2 OFFSET $3
)
SELECT EXISTS(SELECT 1 FROM generation_tasks WHERE id=$1),(SELECT count(*) FROM reviews),
 coalesce((SELECT to_jsonb(r) FROM reviews r ORDER BY revision DESC LIMIT 1),
  '{"id":"","task_id":"","revision":0,"admin_id":"","status":"pending","reason":"","evidence":"","created_at":""}'::jsonb),
 coalesce((SELECT jsonb_agg(to_jsonb(page) ORDER BY revision DESC) FROM page),'[]'::jsonb)`
