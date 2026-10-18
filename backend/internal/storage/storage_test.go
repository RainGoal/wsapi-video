package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLocalSignedContentRangeAndTamper(t *testing.T) {
	s := &Local{root: t.TempDir(), secret: []byte(strings.Repeat("x", 32))}
	ctx := context.Background()
	if e := s.Put(ctx, "usr_one/example.txt", strings.NewReader("0123456789"), "text/plain"); e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	s.origin = server.URL
	link, e := s.SignedURL(ctx, "usr_one/example.txt", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", link, nil)
	req.Header.Set("Range", "bytes=2-5")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(got) != "2345" {
		t.Fatalf("range response %d %q", resp.StatusCode, got)
	}
	u, _ := url.Parse(link)
	q := u.Query()
	q.Set("signature", strings.Repeat("0", 64))
	u.RawQuery = q.Encode()
	resp, e = http.Get(u.String())
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("tampered signature was accepted")
	}
}

func TestLocalRejectsTraversalAndExpiredSignature(t *testing.T) {
	s := &Local{root: t.TempDir(), secret: []byte(strings.Repeat("y", 32))}
	for _, key := range []string{"../escape", "a/../../escape", "/absolute", "C:/absolute", "a\\..\\escape", "a//b"} {
		if _, e := s.path(key); e == nil {
			t.Errorf("accepted unsafe key %q", key)
		}
	}
	mux := http.NewServeMux()
	s.Register(mux)
	request := httptest.NewRequest("GET", "/api/v1/storage/YQ?expires=1&signature="+s.signature("YQ", "1"), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, request)
	if w.Code != 404 {
		t.Fatal("expired URL accepted")
	}
}
