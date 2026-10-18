package projects

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/storage"
)

func TestExportTokenScopeExpiryAndTampering(t *testing.T) {
	svc := &Service{exportSecret: []byte(strings.Repeat("x", 40))}
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	ids := []string{"ast_first", "ast_second"}
	token, expires, err := svc.newExportToken("usr_owner", "prj_owned", ids, now)
	if err != nil || !expires.Equal(now.Add(5*time.Minute)) {
		t.Fatal("could not create five-minute export snapshot", expires, err)
	}
	got, err := svc.parseExportToken(token, "usr_owner", "prj_owned", now.Add(time.Second))
	if err != nil || !reflect.DeepEqual(got, ids) {
		t.Fatal("export snapshot lost its ordered asset IDs", got, err)
	}
	changed := "A" + token[1:]
	if token[0] == 'A' {
		changed = "B" + token[1:]
	}
	for _, tc := range []struct {
		name, token, user, project string
		now                        time.Time
	}{
		{"other account", token, "usr_other", "prj_owned", now},
		{"other project", token, "usr_owner", "prj_other", now},
		{"expired boundary", token, "usr_owner", "prj_owned", expires},
		{"expired later", token, "usr_owner", "prj_owned", expires.Add(time.Hour)},
		{"future expiry", token, "usr_owner", "prj_owned", now.Add(-time.Minute)},
		{"tampered snapshot", changed, "usr_owner", "prj_owned", now},
		{"tampered signature", token + "A", "usr_owner", "prj_owned", now},
		{"missing signature", strings.Split(token, ".")[0], "usr_owner", "prj_owned", now},
		{"overlong input", strings.Repeat("A", maxExportTokenLength+1), "usr_owner", "prj_owned", now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.parseExportToken(tc.token, tc.user, tc.project, tc.now); err == nil {
				t.Fatal("invalid export link was accepted")
			}
		})
	}
	rotated := &Service{exportSecret: []byte(strings.Repeat("y", 40))}
	if _, err := rotated.parseExportToken(token, "usr_owner", "prj_owned", now); err == nil {
		t.Fatal("an export link survived master-secret rotation")
	}
	if _, _, err := (&Service{}).newExportToken("usr_owner", "prj_owned", ids, now); err == nil {
		t.Fatal("export links must fail closed without a configured signing secret")
	}
}

func TestExportTokenSnapshotBounds(t *testing.T) {
	svc := &Service{exportSecret: []byte(strings.Repeat("x", 40))}
	now := time.Now()
	for _, ids := range [][]string{nil, {""}, {"ast_duplicate", "ast_duplicate"}, {strings.Repeat("a", 101)}} {
		token, _, err := svc.newExportToken("usr_owner", "prj_owned", ids, now)
		if err == nil {
			if _, err = svc.parseExportToken(token, "usr_owner", "prj_owned", now); err == nil {
				t.Fatal("invalid asset snapshot was accepted", ids)
			}
		}
	}
	if _, _, err := svc.newExportToken("usr_owner", "prj_owned", make([]string, maxExportAssets+1), now); err == nil {
		t.Fatal("oversized export snapshot was accepted")
	}
}

func TestExportTokenMaximumAssetURLFitsCommonProxyLimit(t *testing.T) {
	svc := &Service{exportSecret: []byte(strings.Repeat("x", 40))}
	ids := make([]string, maxExportAssets)
	for i := range ids {
		ids[i] = core.ID("ast")
	}
	now, userID, projectID := time.Now(), core.ID("usr"), core.ID("prj")
	token, _, err := svc.newExportToken(userID, projectID, ids, now)
	if err != nil {
		t.Fatal(err)
	}
	requestLine := "GET /api/v1/projects/" + projectID + "/export/download?token=" + token + " HTTP/1.1\r\n"
	if len(requestLine) >= 8<<10 {
		t.Fatalf("200-asset export requires a %d-byte request line, exceeding common proxy limits", len(requestLine))
	}
	got, err := svc.parseExportToken(token, userID, projectID, now)
	if err != nil || !reflect.DeepEqual(got, ids) {
		t.Fatal("full export snapshot did not round-trip", err)
	}
	t.Logf("200-asset request line: %d bytes", len(requestLine))
}

type exportZIPCloseErrorWriter struct{ *httptest.ResponseRecorder }

func (w exportZIPCloseErrorWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte{'P', 'K', 1, 2}) {
		return 0, errors.New("fixture connection failed during ZIP central directory")
	}
	return w.ResponseRecorder.Write(p)
}

func TestExportZIPFinalWriteFailureAbortsHandler(t *testing.T) {
	local, err := storage.New(config.Config{StorageDriver: "local", StorageDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("transport-fixture"), 1024)
	if err = local.Put(context.Background(), "fixture.bin", bytes.NewReader(body), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	objects := &exportTrackedStore{Store: local}
	svc := &Service{blobs: objects}
	defer func() {
		if outcome := recover(); outcome != http.ErrAbortHandler {
			t.Fatal("ZIP final write failure did not abort the HTTP handler", outcome)
		}
		if objects.opened.Load() != 1 || objects.closed.Load() != 1 {
			t.Fatal("ZIP final write failure retained its object reader")
		}
	}()
	svc.streamExport(exportZIPCloseErrorWriter{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/export/download", nil), exportManifest{files: []exportFile{{id: "fixture", key: "fixture.bin", name: "fixture.bin", size: int64(len(body))}}})
}
