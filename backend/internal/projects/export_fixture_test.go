package projects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/internal/storage"
	"wsapi-video/migrations"
)

type exportFixture struct {
	t               *testing.T
	db              *pgxpool.Pool
	userID, project string
	objects         *exportTrackedStore
	host            *httptest.Server
	done            chan struct{}
}

func newExportFixture(t *testing.T) *exportFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for streaming export integration")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"test_export_" + strings.TrimPrefix(core.ID(""), "_")}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	f := &exportFixture{t: t, db: db, userID: core.ID("usr"), project: core.ID("prj"), done: make(chan struct{}, 32)}
	if _, err = db.Exec(ctx, "INSERT INTO users(id,email,password_hash,display_name,email_verified) VALUES($1,$2,'fixture','Export fixture',true)", f.userID, f.userID+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "INSERT INTO wallet_accounts(user_id,available) VALUES($1,1234)", f.userID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "INSERT INTO projects(id,user_id,name) VALUES($1,$2,'Large export fixture')", f.project, f.userID); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{StorageDriver: "local", StorageDir: t.TempDir(), AppSecret: strings.Repeat("export-test", 4)}
	local, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.objects = &exportTrackedStore{Store: local}
	mux := http.NewServeMux()
	identity := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/export/download") {
				defer func() { f.done <- struct{}{} }()
			}
			next.ServeHTTP(w, r.WithContext(core.WithUser(r.Context(), core.User{ID: f.userID})))
		})
	}
	New(db, cfg, f.objects).Register(mux, identity, identity)
	f.host = httptest.NewServer(mux)
	t.Cleanup(f.host.Close)
	return f
}

func (f *exportFixture) seedObject(id string, size int64) string {
	f.t.Helper()
	block := make([]byte, 64<<10)
	for i := range block {
		block[i] = byte(i*17 + i/257)
	}
	// Repeated deterministic media bytes test transport only, not video decoding.
	body := &exportRepeatReader{block: block, remaining: size}
	hash := sha256.New()
	key := f.userID + "/" + f.project + "/" + id + ".bin"
	if err := f.objects.Put(context.Background(), key, io.TeeReader(body, hash), "application/octet-stream"); err != nil {
		f.t.Fatal(err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	_, err := f.db.Exec(context.Background(), "INSERT INTO assets(id,user_id,project_id,kind,file_name,content_type,object_key,size_bytes,sha256) VALUES($1,$2,$3,'video',$4,'application/octet-stream',$5,$6,$7)", id, f.userID, f.project, id+".bin", key, size, digest)
	if err != nil {
		f.t.Fatal(err)
	}
	return digest
}

func (f *exportFixture) prepare(ids ...string) string {
	f.t.Helper()
	body, _ := json.Marshal(exportInput{AssetIDs: ids})
	response, err := http.Post(f.host.URL+"/api/v1/projects/"+f.project+"/export/prepare", "application/json", bytes.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	var answer struct {
		Data struct {
			URL string `json:"download_url"`
		} `json:"data"`
	}
	if err = json.NewDecoder(response.Body).Decode(&answer); err != nil || response.StatusCode != 200 || answer.Data.URL == "" {
		f.t.Fatal("prepare failed", response.StatusCode, err)
	}
	return f.host.URL + answer.Data.URL
}

func (f *exportFixture) financialState() [6]int64 {
	f.t.Helper()
	var state [6]int64
	err := f.db.QueryRow(context.Background(), `SELECT available,frozen,(SELECT count(*) FROM wallet_ledger),(SELECT count(*) FROM generation_tasks),(SELECT count(*) FROM payment_orders),(SELECT count(*) FROM assets) FROM wallet_accounts WHERE user_id=$1`, f.userID).Scan(&state[0], &state[1], &state[2], &state[3], &state[4], &state[5])
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}

type exportRepeatReader struct {
	block     []byte
	offset    int
	remaining int64
}

func (r *exportRepeatReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	written := 0
	for written < len(p) {
		n := copy(p[written:], r.block[r.offset:])
		written += n
		r.offset = (r.offset + n) % len(r.block)
	}
	r.remaining -= int64(written)
	return written, nil
}

type exportTrackedStore struct {
	storage.Store
	mu             sync.RWMutex
	mode           string
	opened, closed atomic.Int64
	readBytes      atomic.Int64
	peakHeap       atomic.Uint64
}

func (s *exportTrackedStore) setMode(mode string) {
	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()
}

func (s *exportTrackedStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	body, err := s.Store.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	mode := s.mode
	s.mu.RUnlock()
	s.opened.Add(1)
	return &exportTrackedReader{ReadCloser: body, store: s, mode: mode}, nil
}

type exportTrackedReader struct {
	io.ReadCloser
	store *exportTrackedStore
	mode  string
	read  int64
	once  sync.Once
}

func (r *exportTrackedReader) Read(p []byte) (int, error) {
	if r.mode == "read_error" || r.mode == "short_eof" {
		remaining := int64(1<<20) - r.read
		if remaining <= 0 {
			if r.mode == "short_eof" {
				return 0, io.EOF
			}
			return 0, errors.New("fixture storage read failed mid-object")
		}
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}
	n, err := r.ReadCloser.Read(p)
	r.read += int64(n)
	after := r.store.readBytes.Add(int64(n))
	if after/(4<<20) != (after-int64(n))/(4<<20) {
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		for previous := r.store.peakHeap.Load(); memory.HeapAlloc > previous; previous = r.store.peakHeap.Load() {
			if r.store.peakHeap.CompareAndSwap(previous, memory.HeapAlloc) {
				break
			}
		}
	}
	return n, err
}

func (r *exportTrackedReader) Close() error {
	var err error
	r.once.Do(func() {
		err = r.ReadCloser.Close()
		r.store.closed.Add(1)
	})
	return err
}
