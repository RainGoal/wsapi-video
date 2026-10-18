// Package observability records only fixed operational facts and local IDs.
package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

type Correlation struct {
	TaskID  string
	Attempt int
	OrderID string
}

func NewJSONLogger(out io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{ReplaceAttr: safeAttr}))
}

// Event deliberately has no error, message, request, or arbitrary fields input.
func Event(ctx context.Context, level slog.Level, event, code string, links Correlation) {
	if !knownEvent(event) || !knownCode(code) {
		return
	}
	attrs := []slog.Attr{slog.String("event", event), slog.String("code", code)}
	if localID(links.TaskID, "task_") {
		attrs = append(attrs, slog.String("task_id", links.TaskID))
		if links.Attempt > 0 {
			attrs = append(attrs, slog.Int("attempt", links.Attempt))
		}
	}
	if localID(links.OrderID, "order_") {
		attrs = append(attrs, slog.String("order_id", links.OrderID))
	}
	slog.LogAttrs(ctx, level, "platform_event", attrs...)
}

func safeAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) != 0 {
		return slog.Attr{}
	}
	switch a.Key {
	case slog.TimeKey:
		if a.Value.Kind() == slog.KindTime {
			return a
		}
	case slog.LevelKey:
		if _, ok := a.Value.Any().(slog.Level); ok {
			return a
		}
	case slog.MessageKey:
		// SetDefault also routes legacy standard-library logs through this handler.
		return slog.String(slog.MessageKey, "platform_event")
	case "event":
		if a.Value.Kind() == slog.KindString && knownEvent(a.Value.String()) {
			return a
		}
	case "code":
		if a.Value.Kind() == slog.KindString && knownCode(a.Value.String()) {
			return a
		}
	case "task_id", "order_id":
		prefix := strings.TrimSuffix(a.Key, "_id") + "_"
		if a.Value.Kind() == slog.KindString && localID(a.Value.String(), prefix) {
			return a
		}
	case "attempt":
		if a.Value.Kind() == slog.KindInt64 && a.Value.Int64() > 0 {
			return a
		}
	}
	return slog.Attr{}
}

func localID(value, prefix string) bool {
	if len(value) != len(prefix)+32 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, ch := range value[len(prefix):] {
		if !('0' <= ch && ch <= '9') && !('a' <= ch && ch <= 'f') {
			return false
		}
	}
	return true
}

func knownEvent(value string) bool {
	switch value {
	case "server_lifecycle", "request_failed", "worker_failed", "task_created", "task_claimed", "task_submission", "task_review", "task_retry", "task_state_write_failed", "task_settlement", "task_settlement_failed", "payment_order_created", "payment_confirmation_failed", "payment_credited", "payment_notification_rejected", "payment_reconcile_failed":
		return true
	}
	return false
}

func knownCode(value string) bool {
	switch value {
	case "starting", "stopped", "run_failed", "panic", "claim_failed", "recovery_failed", "attempt_finish_failed", "prepare", "poll", "archive", "submitting", "accepted", "frozen", "consumed", "released", "database_error", "adapter_unavailable", "payment_query_failed", "payment_mismatch", "payment_trade_missing", "invalid_input", "invalid_signature", "order_scan_failed", "submission_unknown", "generation_overdue", "upstream_review", "preparation_overdue", "demo_disabled", "archive_review", "invalid_outputs", "reference_unavailable", "poll_retry", "archive_retry", "archive_kind_mismatch":
		return true
	}
	return false
}
