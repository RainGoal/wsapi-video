package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestConnectorRejectsVideoImageSpecificationsBeforeSending(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if r.URL.Path == "/v1/videos/generations" {
			if payload["resolution"] != "720p" || payload["size"] != nil || payload["quality"] != nil {
				t.Errorf("invalid video payload: %#v", payload)
			}
		} else if payload["size"] != "1024x1024" || payload["quality"] != "high" {
			t.Errorf("image specifications were lost: %#v", payload)
		}
		_, _ = w.Write([]byte(`{"id":"original-job","status":"queued"}`))
	}))
	defer srv.Close()
	c, err := NewConnector(Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"fixture": "fixture-only"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []Input{
		{Kind: "image", Model: "fixture-image", Prompt: "test", Size: "1024x1024", Quality: "high"},
		{Kind: "video", Model: "fixture-video", Prompt: "test", Resolution: "720p"},
	} {
		if _, err := c.Submit(context.Background(), "fixture", in); err != nil {
			t.Fatal(err)
		}
	}
	for name, in := range map[string]Input{
		"quality": {Kind: "video", Quality: "high"},
		"size":    {Kind: "video", Size: "720p"},
		"both":    {Kind: "video", Size: "1024x1024", Quality: "high"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := c.Submit(context.Background(), "fixture", in)
			var rejected *GatewayError
			if !errors.As(err, &rejected) || rejected.Status != http.StatusUnprocessableEntity {
				t.Fatalf("video image specifications were not definitively rejected: %v", err)
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("unsupported fields reached the gateway: calls=%d", got)
			}
		})
	}
}
