package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/core"
	"wsapi-video/internal/httpx"
	"wsapi-video/internal/media"
	"wsapi-video/internal/observability"
)

func (s *Service) pollInterval() time.Duration {
	if s.cfg.MediaPollInterval > 0 {
		return s.cfg.MediaPollInterval
	}
	return 5 * time.Second
}
func (s *Service) leaseDuration() time.Duration {
	if s.cfg.WorkerLease >= 2*time.Minute {
		return s.cfg.WorkerLease
	}
	return 2 * time.Minute
}
func (s *Service) reviewAfter() time.Duration {
	if s.cfg.TaskReviewAfter > 0 {
		return s.cfg.TaskReviewAfter
	}
	return 24 * time.Hour
}

// Run starts bounded workers; every claim is persisted and fenced by its lease.
// A create request is attempted exactly once. Only polling and archiving retry.
func (s *Service) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timer := time.NewTimer(0)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					s.recoverSending(ctx)
					t, err := s.claim(ctx)
					if err == nil {
						s.process(ctx, t)
						timer.Reset(25 * time.Millisecond)
					} else {
						if !errors.Is(err, pgx.ErrNoRows) && ctx.Err() == nil {
							observability.Event(ctx, slog.LevelWarn, "worker_failed", "claim_failed", observability.Correlation{})
						}
						timer.Reset(s.pollInterval())
					}
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.reconcilePayments(ctx)
			}
		}
	}()
	<-ctx.Done()
	wg.Wait()
}

func (s *Service) recoverSending(ctx context.Context) {
	// An expired sending lease cannot prove that the gateway did not accept it.
	if _, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='submission_unknown',error_code='submission_unknown',error_message='提交结果待核查，系统不会重复生成',updated_at=now() WHERE execution_status='sending' AND upstream_id IS NULL AND lease_until<now()"); err != nil {
		observability.Event(ctx, slog.LevelWarn, "worker_failed", "recovery_failed", observability.Correlation{})
	}
}
func (s *Service) claim(ctx context.Context) (Task, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx)
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM generation_tasks WHERE settlement_status='frozen' AND next_attempt_at<=now() AND (lease_until IS NULL OR lease_until<now()) AND (execution_status IN ('queued','generating') OR (execution_status='completed' AND delivery_status IN ('pending','archiving','archive_retry'))) ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT 1"))
	if err != nil {
		return Task{}, err
	}
	t.LeaseToken = core.ID("lease")
	t.Attempts++
	action := "poll"
	if t.ExecutionStatus == "queued" {
		action = "prepare"
	}
	if t.ExecutionStatus == "completed" {
		action = "archive"
	}
	_, err = tx.Exec(ctx, "UPDATE generation_tasks SET lease_token=$2,lease_until=$3,attempts=$4,updated_at=now() WHERE id=$1", t.ID, t.LeaseToken, time.Now().Add(s.leaseDuration()), t.Attempts)
	if err != nil {
		return Task{}, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO task_attempts(id,task_id,attempt,action,status) VALUES($1,$2,$3,$4,'running')", core.ID("attempt"), t.ID, t.Attempts, action)
	if err != nil {
		return Task{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	observability.Event(ctx, slog.LevelInfo, "task_claimed", action, observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	return t, nil
}
func (s *Service) finishAttempt(ctx context.Context, t Task, status string) {
	if _, err := s.db.Exec(ctx, "UPDATE task_attempts SET status=$3,completed_at=now() WHERE task_id=$1 AND attempt=$2", t.ID, t.Attempts, status); err != nil {
		observability.Event(ctx, slog.LevelWarn, "worker_failed", "attempt_finish_failed", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	}
}
func (s *Service) process(ctx context.Context, t Task) {
	defer s.finishAttempt(ctx, t, "finished")
	if t.ExecutionStatus == "completed" {
		s.archive(ctx, t)
		return
	}
	if t.ExecutionStatus == "queued" {
		s.send(ctx, t)
		return
	}
	if time.Since(t.CreatedAt) > s.reviewAfter() {
		s.holdReview(ctx, t, "generation_overdue", "生成时间较长，已转人工核查")
		return
	}
	result, err := s.gateway.Poll(ctx, t.CredentialRef, t.Kind, t.UpstreamID)
	if err != nil {
		var ge *media.GatewayError
		if errors.As(err, &ge) && (ge.Status == 401 || ge.Status == 403 || ge.Status == 404) {
			s.holdReview(ctx, t, "upstream_review", "原生成任务暂不可查询，需要核查原服务凭证与网关任务")
			return
		}
		s.retry(ctx, t, "poll_retry", "生成状态暂不可查询", false)
		return
	}
	switch result.State {
	case "completed":
		outputs, _ := json.Marshal(result.Outputs)
		tag, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='completed',delivery_status='pending',outputs=$3,error_code='',error_message='',updated_at=now() WHERE id=$1 AND lease_token=$2 AND settlement_status='frozen'", t.ID, t.LeaseToken, outputs)
		if err == nil && tag.RowsAffected() == 1 {
			t.ExecutionStatus = "completed"
			t.Outputs = result.Outputs
			s.archive(ctx, t)
		}
	case "failed", "canceled":
		_ = s.settle(ctx, t.ID, t.LeaseToken, "released", result.State, "error", "generation_failed", "生成未完成，预冻结积分已释放")
	default:
		_, _ = s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='generating',lease_until=NULL,lease_token=NULL,next_attempt_at=$3,error_code='',error_message='',updated_at=now() WHERE id=$1 AND lease_token=$2", t.ID, t.LeaseToken, time.Now().Add(s.pollInterval()))
	}
}

func (s *Service) buildGatewayInput(ctx context.Context, t Task) (media.Input, error) {
	in := t.Input
	out := media.Input{Kind: t.Kind, Model: t.Model, Prompt: in.Prompt, Duration: in.Duration, Resolution: in.Resolution, AspectRatio: in.AspectRatio, Size: in.Size, Quality: in.Quality, Audio: in.Audio}
	seen := map[string]bool{}
	for _, id := range in.ReferenceAssetIDs {
		if id == "" || seen[id] {
			return out, errors.New("invalid duplicate reference")
		}
		seen[id] = true
		a, err := s.assets.Resolve(ctx, t.UserID, id)
		if err != nil {
			return out, err
		}
		u, err := s.assets.ReferenceURL(ctx, t.UserID, id)
		if err != nil {
			return out, err
		}
		switch a.Kind {
		case "image":
			out.Images = append(out.Images, u)
		case "video":
			out.Videos = append(out.Videos, u)
		case "audio":
			out.Audios = append(out.Audios, u)
		default:
			return out, errors.New("unsupported reference asset")
		}
	}
	if in.StartFrameAssetID != "" {
		u, err := s.assets.ReferenceURL(ctx, t.UserID, in.StartFrameAssetID)
		if err != nil {
			return out, err
		}
		out.StartFrame = u
	}
	if in.EndFrameAssetID != "" {
		u, err := s.assets.ReferenceURL(ctx, t.UserID, in.EndFrameAssetID)
		if err != nil {
			return out, err
		}
		out.EndFrame = u
	}
	return out, nil
}
func (s *Service) send(ctx context.Context, t Task) {
	if time.Since(t.CreatedAt) > s.reviewAfter() {
		s.holdReview(ctx, t, "preparation_overdue", "生成准备时间较长，尚未重复发送，等待人工核查")
		return
	}
	if t.Demo {
		if !s.isDemo() {
			s.holdReview(ctx, t, "demo_disabled", "演示模式已关闭")
			return
		}
		if t.Kind != "image" {
			_ = s.settle(ctx, t.ID, t.LeaseToken, "released", "failed", "error", "demo_unsupported", "视频演示未启用，积分已释放")
			return
		}
		_, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='completed',delivery_status='pending' WHERE id=$1 AND lease_token=$2", t.ID, t.LeaseToken)
		if err == nil {
			t.ExecutionStatus = "completed"
			s.archive(ctx, t)
		}
		return
	}
	input, err := s.buildGatewayInput(ctx, t)
	if err != nil {
		s.retry(ctx, t, "reference_unavailable", "参考素材暂不可读取，尚未提交生成", false)
		return
	}
	// Persist sending before network I/O. A crash from this point is ambiguous.
	tag, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status='sending',updated_at=now() WHERE id=$1 AND lease_token=$2 AND execution_status='queued' AND settlement_status='frozen'", t.ID, t.LeaseToken)
	if err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			observability.Event(ctx, slog.LevelWarn, "task_state_write_failed", "database_error", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
		}
		return
	}
	observability.Event(ctx, slog.LevelInfo, "task_submission", "submitting", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	result, err := s.gateway.Submit(ctx, t.CredentialRef, input)
	if err != nil {
		var unknown *media.UnknownSubmission
		if errors.As(err, &unknown) {
			s.holdReview(ctx, t, "submission_unknown", "提交结果待核查，系统不会重复生成")
			return
		}
		_ = s.settle(ctx, t.ID, t.LeaseToken, "released", "failed", "error", "submission_rejected", "生成服务未受理，预冻结积分已释放")
		return
	}
	// The original lease may persist its accepted ID even after recovery marked it unknown.
	tag, err = s.db.Exec(ctx, "UPDATE generation_tasks SET upstream_id=$3,execution_status='generating',lease_until=NULL,lease_token=NULL,next_attempt_at=$4,error_code='',error_message='',updated_at=now() WHERE id=$1 AND lease_token=$2 AND upstream_id IS NULL AND execution_status IN ('sending','submission_unknown') AND settlement_status='frozen'", t.ID, t.LeaseToken, result.ID, time.Now().Add(s.pollInterval()))
	if err != nil {
		observability.Event(ctx, slog.LevelWarn, "task_state_write_failed", "database_error", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	} else if tag.RowsAffected() == 1 {
		observability.Event(ctx, slog.LevelInfo, "task_submission", "accepted", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	}
}
func (s *Service) holdReview(ctx context.Context, t Task, code, message string) {
	status := t.ExecutionStatus
	if code == "submission_unknown" {
		status = "submission_unknown"
	}
	// Keep the original sending lease token so a late successful response can save its ID.
	tag, err := s.db.Exec(ctx, "UPDATE generation_tasks SET execution_status=$3,procurement_status='disputed',error_code=$4,error_message=$5,lease_until=$6,next_attempt_at=$6,updated_at=now() WHERE id=$1 AND lease_token=$2 AND settlement_status='frozen'", t.ID, t.LeaseToken, status, code, message, time.Now().Add(365*24*time.Hour))
	if err != nil {
		observability.Event(ctx, slog.LevelWarn, "task_state_write_failed", "database_error", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	} else if tag.RowsAffected() == 1 {
		observability.Event(ctx, slog.LevelWarn, "task_review", code, observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	}
}
func (s *Service) retry(ctx context.Context, t Task, code, message string, archive bool) {
	delivery := t.DeliveryStatus
	if archive {
		delivery = "archive_retry"
	}
	tag, err := s.db.Exec(ctx, "UPDATE generation_tasks SET delivery_status=$3,error_code=$4,error_message=$5,lease_until=NULL,lease_token=NULL,next_attempt_at=$6,updated_at=now() WHERE id=$1 AND lease_token=$2 AND settlement_status='frozen'", t.ID, t.LeaseToken, delivery, code, message, time.Now().Add(30*time.Second))
	if err != nil {
		observability.Event(ctx, slog.LevelWarn, "task_state_write_failed", "database_error", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	} else if tag.RowsAffected() == 1 {
		observability.Event(ctx, slog.LevelWarn, "task_retry", code, observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
	}
}

func (s *Service) archive(ctx context.Context, t Task) {
	tag, err := s.db.Exec(ctx, "UPDATE generation_tasks SET delivery_status='archiving',updated_at=now() WHERE id=$1 AND lease_token=$2 AND settlement_status='frozen'", t.ID, t.LeaseToken)
	if err != nil || tag.RowsAffected() != 1 {
		return
	}
	if s.assets != nil {
		if existing, e := s.assets.Archived(ctx, t.UserID, t.ProjectID, t.ID); e == nil {
			s.completeArchive(ctx, t, existing)
			return
		} else if !errors.Is(e, assets.ErrNotFound) {
			if time.Since(t.CreatedAt) > s.reviewAfter() {
				s.holdReview(ctx, t, "archive_review", "已归档作品暂不可读取，等待人工核查")
			} else {
				s.retry(ctx, t, "archive_retry", "已归档作品暂不可读取，保存稍后重试，不会重新生成", true)
			}
			return
		}
	}
	var body io.ReadCloser
	var contentType, fileName string
	if t.Demo {
		body = io.NopCloser(bytes.NewReader(demoPNG(t.ID, t.Input.AspectRatio)))
		contentType = "image/png"
		fileName = "DEMO-" + t.ID + ".png"
	} else if t.Kind == "video" {
		body, contentType, err = s.gateway.OpenVideo(ctx, t.CredentialRef, t.UpstreamID)
		fileName = t.ID + ".mp4"
	} else {
		if len(t.Outputs) != 1 {
			s.holdReview(ctx, t, "invalid_outputs", "生成结果数量异常，等待核查")
			return
		}
		imageGateway := s.gateway
		if s.settings != nil {
			var original *media.Connector
			original, err = s.settings.Gateway(ctx, t.CredentialRef)
			if err == nil {
				imageGateway = original
			}
		}
		if err == nil {
			body, contentType, err = imageGateway.OpenImage(ctx, t.Outputs[0])
		}
		// Refresh one expired URL through the original job before applying the
		// age limit, including an explicit retry of an old completed task.
		var ge *media.GatewayError
		if errors.As(err, &ge) && (ge.Status == 401 || ge.Status == 403 || ge.Status == 404) {
			if refreshed, e := s.gateway.Poll(ctx, t.CredentialRef, t.Kind, t.UpstreamID); e == nil && refreshed.State == "completed" && len(refreshed.Outputs) == 1 && (refreshed.Model == "" || refreshed.Model == t.Model) {
				raw, _ := json.Marshal(refreshed.Outputs)
				tag, e := s.db.Exec(ctx, "UPDATE generation_tasks SET outputs=$3 WHERE id=$1 AND lease_token=$2 AND settlement_status='frozen'", t.ID, t.LeaseToken, raw)
				if e != nil || tag.RowsAffected() != 1 {
					if e != nil {
						observability.Event(ctx, slog.LevelWarn, "task_state_write_failed", "database_error", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
					}
					return
				}
				body, contentType, err = imageGateway.OpenImage(ctx, refreshed.Outputs[0])
			}
		}
		fileName = t.ID + imageExtension(contentType)
	}
	if err != nil {
		retryMessage := "生成已完成，作品保存稍后重试，不会重新生成"
		reviewMessage := "生成已完成，作品暂无法取回，等待人工核查"
		if errors.Is(err, media.ErrOutputOriginDenied) {
			retryMessage = "生成已完成，但图片下载来源未获许可，请管理员配置结果下载来源白名单后重试保存"
			reviewMessage = retryMessage
		}
		if time.Since(t.CreatedAt) > s.reviewAfter() {
			s.holdReview(ctx, t, "archive_review", reviewMessage)
			return
		}
		s.retry(ctx, t, "archive_retry", retryMessage, true)
		return
	}
	defer body.Close()
	if t.Input.ProductSwap != nil && t.Input.ProductSwap.AudioMode != "generated" {
		processed, processErr := s.processSwapAudio(ctx, t, body)
		if processErr != nil {
			s.holdReview(ctx, t, "audio_processing_failed", "视频已生成，但音轨合成失败或时长不匹配，积分保持冻结，等待核查；不会重新生成")
			return
		}
		defer processed.Close()
		body, contentType = processed, "video/mp4"
	}
	a, err := s.assets.Archive(ctx, t.UserID, t.ProjectID, t.ID, fileName, contentType, body)
	if err != nil {
		if time.Since(t.CreatedAt) > s.reviewAfter() {
			s.holdReview(ctx, t, "archive_review", "生成已完成，保存异常等待人工处理")
		} else {
			s.retry(ctx, t, "archive_retry", "生成已完成，作品保存稍后重试，不会重新生成", true)
		}
		return
	}
	s.completeArchive(ctx, t, a)
}

func (s *Service) completeArchive(ctx context.Context, t Task, a assets.Asset) {
	// Both newly saved results and recovered archives must match the purchased
	// media kind before attaching an asset or consuming the frozen credits.
	if a.Kind != t.Kind {
		tag, err := s.db.Exec(ctx, "UPDATE generation_tasks SET delivery_status='error',procurement_status='disputed',error_code='archive_kind_mismatch',error_message='归档作品类型与生成任务不符，积分保持冻结，等待人工核查',lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=$1 AND lease_token=$2 AND settlement_status='frozen'", t.ID, t.LeaseToken)
		if err != nil {
			observability.Event(ctx, slog.LevelWarn, "task_state_write_failed", "database_error", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
		} else if tag.RowsAffected() == 1 {
			observability.Event(ctx, slog.LevelWarn, "task_review", "archive_kind_mismatch", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
		}
		return
	}
	ids, _ := json.Marshal([]string{a.ID})
	tag, err := s.db.Exec(ctx, "UPDATE generation_tasks SET asset_ids=$3 WHERE id=$1 AND lease_token=$2 AND settlement_status='frozen'", t.ID, t.LeaseToken, ids)
	if err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			observability.Event(ctx, slog.LevelWarn, "task_state_write_failed", "database_error", observability.Correlation{TaskID: t.ID, Attempt: t.Attempts})
		}
		return
	}
	_ = s.settle(ctx, t.ID, t.LeaseToken, "consumed", "completed", "ready", "", "作品已保存，积分消费已结算")
}

func imageExtension(contentType string) string {
	ct, _, _ := mime.ParseMediaType(contentType)
	switch ct {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

// demoPNG is a deliberately abstract, visibly marked fixture, not an AI result.
func demoPNG(seed, ratio string) []byte {
	w, h := 1024, 576
	if ratio == "1:1" {
		h = 1024
	}
	if ratio == "9:16" {
		w, h = 576, 1024
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8((x*23/w + y*31/h) % 60)
			img.SetRGBA(x, y, color.RGBA{18 + v/2, 22 + v/2, 26 + v, 255})
		}
	}
	// Pixel lettering makes the DEMO watermark intrinsic to the saved media.
	letters := map[byte][]string{'D': {"11110", "10001", "10001", "10001", "11110"}, 'E': {"11111", "10000", "11110", "10000", "11111"}, 'M': {"10001", "11011", "10101", "10001", "10001"}, 'O': {"01110", "10001", "10001", "10001", "01110"}}
	scale := 12
	startX := (w - 23*scale) / 2
	startY := (h - 5*scale) / 2
	for i, ch := range []byte("DEMO") {
		for y, row := range letters[ch] {
			for x, bit := range row {
				if bit == '1' {
					for yy := 0; yy < scale; yy++ {
						for xx := 0; xx < scale; xx++ {
							img.SetRGBA(startX+(i*6+x)*scale+xx, startY+y*scale+yy, color.RGBA{215, 171, 106, 255})
						}
					}
				}
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func (s *Service) cancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.getTask(ctx, core.UserFrom(ctx).ID, r.PathValue("id"), false)
	if err != nil {
		dbError(w, err)
		return
	}
	if t.SettlementStatus != "frozen" {
		conflict(w, "该任务已结算")
		return
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var execution string
	var leaseUntil *time.Time
	err = tx.QueryRow(ctx, "SELECT execution_status,lease_until FROM generation_tasks WHERE id=$1 FOR UPDATE", t.ID).Scan(&execution, &leaseUntil)
	if err != nil {
		dbError(w, err)
		return
	}
	if execution == "queued" && (leaseUntil == nil || leaseUntil.Before(time.Now())) {
		if err = changeWallet(ctx, tx, t.UserID, t.Credits, -t.Credits, "task:"+t.ID+":released", "released", t.ID, "", "用户取消尚未发送的任务"); err != nil {
			dbError(w, err)
			return
		}
		_, err = tx.Exec(ctx, "UPDATE generation_tasks SET execution_status='canceled',delivery_status='error',settlement_status='released',updated_at=now() WHERE id=$1", t.ID)
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			dbError(w, err)
			return
		}
		observability.Event(ctx, slog.LevelInfo, "task_settlement", "released", observability.Correlation{TaskID: t.ID})
		out, e := s.getTask(ctx, t.UserID, t.ID, false)
		if e != nil {
			dbError(w, e)
			return
		}
		httpx.JSON(w, 200, out)
		return
	}
	_ = tx.Rollback(ctx)
	if t.Kind != "video" || !t.Product.Capabilities.SupportsCancel || t.UpstreamID == "" {
		httpx.Fail(w, 409, "cancel_unsupported", "此任务当前不支持取消，积分保持原结算规则")
		return
	}
	if err = s.gateway.Cancel(ctx, t.CredentialRef, t.UpstreamID); err != nil {
		httpx.Fail(w, 502, "cancel_unconfirmed", "网关尚未确认取消，请稍后查询原任务")
		return
	}
	// Successful DELETE is not proof of terminal cancellation. Poll the original job.
	_, _ = s.db.Exec(ctx, "UPDATE generation_tasks SET next_attempt_at=now() WHERE id=$1", t.ID)
	out, e := s.getTask(ctx, t.UserID, t.ID, false)
	if e != nil {
		dbError(w, e)
		return
	}
	httpx.JSON(w, 202, out)
}
