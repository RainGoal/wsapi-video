package server

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wsapi-video/internal/config"
	"wsapi-video/internal/observability"
)

func TestRequestPanicLoggingOmitsRequestAndPanicData(t *testing.T) {
	previous, writer, flags, prefix := slog.Default(), log.Writer(), log.Flags(), log.Prefix()
	var out bytes.Buffer
	slog.SetDefault(observability.NewJSONLogger(&out))
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(writer)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
	secret := "SENSITIVE_REQUEST_SENTINEL"
	handler := secure(config.Config{PublicURL: "https://studio.example"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("https://provider.example/" + secret + "?key=" + secret)
	}))
	req := httptest.NewRequest("GET", "/api/"+secret+"?token="+secret, strings.NewReader(secret))
	req.Header.Set("Authorization", secret)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") {
		t.Fatal("panic response changed", rec.Code)
	}
	if strings.Contains(out.String(), secret) || strings.Contains(out.String(), "provider.example") {
		t.Fatal("request or panic value reached logs")
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &record); err != nil {
		t.Fatal("invalid JSON panic record", err)
	}
	if len(record) != 5 || record["event"] != "request_failed" || record["code"] != "panic" || record["msg"] != "platform_event" {
		t.Fatal("unexpected panic record", record)
	}
	out.Reset()
	handler = secure(config.Config{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	var aborted any
	func() {
		defer func() { aborted = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	if aborted != http.ErrAbortHandler || out.Len() != 0 {
		t.Fatal("ErrAbortHandler behavior changed")
	}
}
