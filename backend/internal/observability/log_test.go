package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func captureStructuredLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous, writer, flags, prefix := slog.Default(), log.Writer(), log.Flags(), log.Prefix()
	var out bytes.Buffer
	slog.SetDefault(NewJSONLogger(&out))
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(writer)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
	return &out
}

func structuredRecords(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal("invalid JSON log", err)
		}
		for key := range record {
			switch key {
			case "time", "level", "msg", "event", "code", "task_id", "attempt", "order_id":
			default:
				t.Fatalf("unexpected log field %q", key)
			}
		}
		if record["msg"] != "platform_event" {
			t.Fatal("log message was not fixed")
		}
		records = append(records, record)
	}
	return records
}

func TestStructuredEventRetainsOnlyValidatedCorrelation(t *testing.T) {
	out := captureStructuredLogs(t)
	task := "task_0123456789abcdef0123456789abcdef"
	order := "order_0123456789abcdef0123456789abcdef"
	Event(context.Background(), slog.LevelWarn, "task_retry", "archive_retry", Correlation{TaskID: task, Attempt: 3, OrderID: order})
	records := structuredRecords(t, out)
	if len(records) != 1 {
		t.Fatal("expected one event", len(records))
	}
	r := records[0]
	if r["event"] != "task_retry" || r["code"] != "archive_retry" || r["task_id"] != task || r["order_id"] != order || r["attempt"] != float64(3) || r["level"] != "WARN" {
		t.Fatal("event correlation was lost", r)
	}
}

func TestStructuredHandlerDropsSensitiveInputsAndLegacyMessages(t *testing.T) {
	out := captureStructuredLogs(t)
	secret := "SENSITIVE_LOG_SENTINEL"
	slog.Error(secret,
		"error", errors.New(secret), "url", "https://"+secret+"/signed?key="+secret,
		"key", secret, "prompt", secret, "email", secret+"@example.test",
		"reason", secret, "evidence", secret, "event", secret, "code", secret,
		"task_id", "task_"+secret, "order_id", "order_"+secret,
		slog.Group(secret, slog.String("code", "accepted")))
	slog.Default().WithGroup(secret).With("reason", secret).Error(secret, "task_id", secret)
	log.Print(errors.New(secret))
	if strings.Contains(out.String(), secret) {
		t.Fatal("sensitive value reached structured logs")
	}
	records := structuredRecords(t, out)
	if len(records) != 3 {
		t.Fatal("expected slog and legacy records", len(records))
	}
	for _, record := range records {
		if len(record) != 3 {
			t.Fatal("untrusted fields were retained", record)
		}
	}
}

func TestStructuredEventRejectsUnknownCodesAndMalformedIDs(t *testing.T) {
	out := captureStructuredLogs(t)
	ctx := context.Background()
	Event(ctx, slog.LevelInfo, "secret-event", "accepted", Correlation{})
	Event(ctx, slog.LevelInfo, "task_retry", "secret-code", Correlation{})
	if out.Len() != 0 {
		t.Fatal("unknown event or code was emitted")
	}
	for _, id := range []string{"https://signed.example/key", "user@example.test", "task_0123456789abcdef0123456789abcdef\n", "task_0123456789ABCDEF0123456789abcdef", "task_0123456789abcdef0123456789abcdef0"} {
		Event(ctx, slog.LevelInfo, "task_created", "frozen", Correlation{TaskID: id, Attempt: 4, OrderID: id})
	}
	for _, record := range structuredRecords(t, out) {
		for _, field := range []string{"task_id", "order_id", "attempt"} {
			if _, ok := record[field]; ok {
				t.Fatal("unvalidated correlation reached logs", field)
			}
		}
	}
}
