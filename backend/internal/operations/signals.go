package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/httpx"
)

type SignalSample struct {
	ID         string    `json:"id"`
	OccurredAt time.Time `json:"occurred_at"`
	Attempts   int       `json:"attempts"`
}

type Signal struct {
	Code     string         `json:"code"`
	Count    int64          `json:"count"`
	OldestAt *time.Time     `json:"oldest_at"`
	Samples  []SignalSample `json:"samples"`
}

type SnapshotResult struct {
	GeneratedAt        time.Time `json:"generated_at"`
	ReviewAfterSeconds float64   `json:"review_after_seconds"`
	SampleLimit        int       `json:"sample_limit"`
	Signals            []Signal  `json:"signals"`
}

// Snapshot only reads local IDs, times and attempt counts. Samples are bounded
// examples, not a complete object list. Creation time is used where no distinct
// incident timestamp exists; expired leases use their expiry time instead.
func Snapshot(ctx context.Context, db *pgxpool.Pool, reviewAfter time.Duration) (SnapshotResult, error) {
	if reviewAfter <= 0 {
		reviewAfter = 24 * time.Hour
	}
	var raw []byte
	err := db.QueryRow(ctx, signalsSQL, reviewAfter.Seconds()).Scan(&raw)
	if err != nil {
		return SnapshotResult{}, err
	}
	var result SnapshotResult
	err = json.Unmarshal(raw, &result)
	return result, err
}

func (s *Service) signals(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := Snapshot(ctx, s.db, s.reviewAfter)
	if err != nil {
		httpx.Fail(w, 503, "signals_unavailable", "暂时无法读取运营异常快照")
		return
	}
	httpx.JSON(w, 200, result)
}

const signalsSQL = `WITH clock AS (SELECT statement_timestamp() AS generated_at),
codes(code, ordinal) AS (VALUES
 ('task_submission_unknown',1),('task_frozen_overdue',2),('task_archive_failed',3),
 ('task_lease_expired',4),('payment_review',5),('mail_pending',6),('mail_retry',7),
 ('procurement_review_disputed',8)
), matches AS (
 SELECT flags.code,t.id,CASE WHEN flags.code='task_lease_expired' THEN t.lease_until ELSE t.created_at END AS occurred_at,t.attempts
 FROM generation_tasks t CROSS JOIN clock c CROSS JOIN LATERAL (VALUES
  ('task_submission_unknown',t.execution_status='submission_unknown'),
  ('task_frozen_overdue',t.created_at<c.generated_at-$1::double precision*interval '1 second'),
  ('task_archive_failed',t.execution_status='completed' AND (t.delivery_status IN ('archive_retry','error') OR t.error_code='archive_review')),
  ('task_lease_expired',t.lease_until IS NOT NULL AND t.lease_until<c.generated_at)
 ) AS flags(code, included) WHERE t.settlement_status='frozen' AND flags.included
 UNION ALL
 SELECT 'payment_review',p.id,p.created_at,p.check_attempts FROM payment_orders p CROSS JOIN clock c
 WHERE p.credit_status='pending' AND (p.payment_status='paid' OR p.expires_at<c.generated_at OR p.check_attempts>=3)
 UNION ALL
 SELECT flags.code,m.id,m.created_at,m.attempts FROM auth_mail_outbox m
 CROSS JOIN LATERAL (VALUES ('mail_pending',true),('mail_retry',m.attempts>0)) AS flags(code,included)
 WHERE m.sent_at IS NULL AND m.cancelled_at IS NULL AND flags.included
 UNION ALL
 SELECT 'procurement_review_disputed',p.task_id,p.created_at,0 FROM task_procurement_reviews p
 WHERE p.status='disputed' AND NOT EXISTS (
  SELECT 1 FROM task_procurement_reviews newer WHERE newer.task_id=p.task_id AND newer.revision>p.revision
 )
), ranked AS (
 SELECT *,row_number() OVER (PARTITION BY code ORDER BY occurred_at,id) AS position FROM matches
), totals AS (
 SELECT code,count(*) AS count,min(occurred_at) AS oldest_at,
  jsonb_agg(jsonb_build_object('id',id,'occurred_at',occurred_at,'attempts',attempts) ORDER BY occurred_at,id)
  FILTER (WHERE position<=5) AS samples FROM ranked GROUP BY code
)
SELECT jsonb_build_object('generated_at',c.generated_at,'review_after_seconds',$1::double precision,'sample_limit',5,
 'signals',(SELECT jsonb_agg(jsonb_build_object('code',codes.code,'count',coalesce(t.count,0),
  'oldest_at',t.oldest_at,'samples',coalesce(t.samples,'[]'::jsonb)) ORDER BY codes.ordinal)
  FROM codes LEFT JOIN totals t USING(code))) FROM clock c`
