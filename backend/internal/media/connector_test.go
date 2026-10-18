package media

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFixedConnectorPreservesCredentialAndPayload(t *testing.T) {
	var seen map[string]any
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		path = r.URL.Path
		if r.Method == "POST" {
			_ = json.NewDecoder(r.Body).Decode(&seen)
			_, _ = io.WriteString(w, `{"id":"opaque-123","status":"queued"}`)
		} else {
			_, _ = io.WriteString(w, `{"id":"opaque-123","status":"completed","result":{"data":[{"url":"https://objects.example/result.png"}]}}`)
		}
	}))
	defer srv.Close()
	c, err := NewConnector(Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"old": "original-key", "new": "rotated-key"}, ImageCredential: "new"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Submit(context.Background(), "old", Input{Kind: "image", Model: "allowed-model", Prompt: "hello", Images: []string{"https://private.example/signed"}, Size: "1024x1024"})
	if err != nil || r.ID != "opaque-123" {
		t.Fatalf("submit: %#v %v", r, err)
	}
	if auth != "Bearer original-key" || path != "/v1/images/edits/async" || seen["n"] != float64(1) {
		t.Fatalf("invalid frozen request: %s %s %#v", auth, path, seen)
	}
	if _, ok := seen["referenceImages"]; ok {
		t.Fatal("image endpoint received video fields")
	}
	_, err = c.Poll(context.Background(), "old", "image", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer original-key" || path != "/v1/images/tasks/opaque-123" {
		t.Fatal("poll did not preserve original key/path")
	}
}
func TestSubmissionUnknownAndNoRedirect(t *testing.T) {
	var leaks int
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaks++; w.WriteHeader(200) }))
	defer other.Close()
	for _, status := range []int{500, 408, 409} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer srv.Close()
			c, _ := NewConnector(Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"k": "secret"}})
			_, err := c.Submit(context.Background(), "k", Input{Kind: "video"})
			var u *UnknownSubmission
			if !errors.As(err, &u) {
				t.Fatalf("must retain unknown submission: %v", err)
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) }))
	defer srv.Close()
	c, _ := NewConnector(Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"k": "secret"}})
	_, _, err := c.OpenVideo(context.Background(), "k", "task")
	if err == nil || leaks != 0 {
		t.Fatalf("redirect leaked credentials: %d %v", leaks, err)
	}
}
func TestOutputOriginAndBoundedRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("image output received service credential")
		}
		w.Header().Set("Content-Type", "image/png")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, strings.Repeat("x", 20))
	}))
	defer srv.Close()
	c, _ := NewConnector(Config{Origin: srv.URL, AllowHTTP: true, MaxMediaBytes: 10})
	if _, _, err := c.OpenImage(context.Background(), Output{URL: "https://attacker.example/x"}); err == nil {
		t.Fatal("untrusted output accepted")
	}
	r, _, err := c.OpenImage(context.Background(), Output{URL: srv.URL + "/output"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = io.ReadAll(r); err == nil {
		t.Fatal("chunked media exceeded limit")
	}
	for _, id := range []string{"../x", "x/y", "x%2fy", "x?secret", "x#fragment"} {
		if validTaskID(id) {
			t.Fatalf("unsafe task path accepted: %s", id)
		}
	}
}

func TestPollRejectsMismatchedIdentity(t *testing.T) {
	response := `{"id":"wrong-job","status":"completed","result":{"data":[{"url":"https://objects.example/a.png"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, response) }))
	defer srv.Close()
	c, _ := NewConnector(Config{Origin: srv.URL, AllowHTTP: true, Keys: map[string]string{"k": "secret"}})
	if _, err := c.Poll(context.Background(), "k", "image", "original-job"); err == nil {
		t.Fatal("another job's output was accepted")
	}
	response = `{"task_id":"original-job","status":"processing"}`
	if _, err := c.Poll(context.Background(), "k", "image", "original-job"); err != nil {
		t.Fatal(err)
	}
}
