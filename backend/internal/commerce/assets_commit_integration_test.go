package commerce

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/assets"
	"wsapi-video/internal/config"
	"wsapi-video/internal/storage"
)

// Drop only the COMMIT exchange. In after mode the complete ReadyForQuery
// response is consumed first, proving PostgreSQL committed before the loss.
type commitLossConn struct {
	net.Conn
	armed   *atomic.Bool
	dropped *atomic.Int64
	after   bool
	pending atomic.Bool
}

func (c *commitLossConn) Write(p []byte) (int, error) {
	if len(p) >= 5 && p[0] == 'Q' && bytes.Equal(p[5:], []byte("commit\x00")) && c.armed.CompareAndSwap(true, false) {
		c.dropped.Add(1)
		if !c.after {
			_ = c.Conn.Close()
			return 0, io.ErrClosedPipe
		}
		c.pending.Store(true)
	}
	return c.Conn.Write(p)
}

func (c *commitLossConn) Read(p []byte) (int, error) {
	if !c.pending.CompareAndSwap(true, false) {
		return c.Conn.Read(p)
	}
	defer c.Conn.Close()
	for {
		var header [5]byte
		if _, err := io.ReadFull(c.Conn, header[:]); err != nil {
			return 0, err
		}
		length := binary.BigEndian.Uint32(header[1:])
		if length < 4 || length > 1<<20 {
			return 0, io.ErrUnexpectedEOF
		}
		if _, err := io.CopyN(io.Discard, c.Conn, int64(length)-4); err != nil {
			return 0, err
		}
		if header[0] == 'Z' {
			return 0, io.ErrUnexpectedEOF
		}
	}
}

type commitObjectStore struct {
	storage.Store
	key     string
	deletes int
}

func (s *commitObjectStore) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	s.key = key
	return s.Store.Put(ctx, key, body, contentType)
}
func (s *commitObjectStore) Delete(ctx context.Context, key string) error {
	s.deletes++
	return s.Store.Delete(ctx, key)
}

func TestPostgresAssetCommitLossPreservesStoredObject(t *testing.T) {
	for _, mode := range []string{"after", "before", "insert"} {
		t.Run(mode, func(t *testing.T) {
			db := integrationDB(t)
			ctx := context.Background()
			user := seedUser(t, db, 100)
			project := seedProject(t, db, user)
			seedRealProduct(t, db)
			cfg := config.Config{StorageDriver: "local", StorageDir: t.TempDir(), MaxMediaBytes: 8 << 20, Sub2APIOrigin: "https://gateway.example", ImageCredential: "original", MediaKeys: map[string]string{"original": "secret"}}
			jobs := New(db, cfg, nil)
			quote := createQuote(t, jobs, user, project, "real-image")
			rec := doRequest(jobs, user, "POST", "/api/v1/tasks", map[string]string{"quote_id": quote.ID, "request_key": "lost-commit-task"})
			if rec.Code != 201 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			task := data[Task](t, rec)
			poolConfig := db.Config()
			if poolConfig.ConnConfig.TLSConfig != nil {
				t.Skip("wire fault fixture requires a local non-TLS PostgreSQL connection")
			}
			poolConfig.MaxConns, poolConfig.MinConns = 1, 0
			dial := poolConfig.ConnConfig.DialFunc
			var armed atomic.Bool
			var dropped atomic.Int64
			poolConfig.ConnConfig.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
				c, err := dial(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				return &commitLossConn{Conn: c, armed: &armed, dropped: &dropped, after: mode == "after"}, nil
			}
			faultDB, err := pgxpool.NewWithConfig(ctx, poolConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer faultDB.Close()
			local, err := storage.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			objects := &commitObjectStore{Store: local}
			archive := assets.New(faultDB, cfg, objects)
			png := demoPNG(task.ID, "16:9")
			if mode == "insert" {
				if _, err = db.Exec(ctx, "ALTER TABLE assets ADD CONSTRAINT reject_fixture_insert CHECK (file_name <> 'commit.png')"); err != nil {
					t.Fatal(err)
				}
			} else {
				armed.Store(true)
			}
			saved, archiveErr := archive.Archive(ctx, user, project, task.ID, "commit.png", "image/png", bytes.NewReader(png))
			var records int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM assets WHERE user_id=$1", user).Scan(&records); err != nil {
				t.Fatal(err)
			}
			reader, openErr := objects.Open(ctx, objects.key)
			if mode == "insert" {
				if reader != nil {
					reader.Close()
				}
				if archiveErr == nil || records != 0 || objects.deletes != 1 || !os.IsNotExist(openErr) || dropped.Load() != 0 {
					t.Fatal("definitely rejected insert retained or delivered an object", archiveErr, records, objects.deletes, openErr, dropped.Load())
				}
				return
			}
			if openErr != nil {
				t.Fatal("unknown commit outcome deleted the stored object", openErr)
			}
			defer reader.Close()
			stored, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(stored, png) || objects.deletes != 0 || dropped.Load() != 1 {
				t.Fatal("commit loss changed the stored bytes or missed the fault", err, objects.deletes, dropped.Load())
			}
			if mode == "after" {
				if archiveErr != nil || records != 1 || saved.ID == "" {
					t.Fatal("committed asset was not recovered after lost response", archiveErr, records)
				}
				recovered, e := archive.Archived(ctx, user, project, task.ID)
				if e != nil || recovered.ID != saved.ID {
					t.Fatal("committed archive could not be reused", e)
				}
			} else if archiveErr == nil || records != 0 {
				t.Fatal("uncommitted object was reported as delivered", archiveErr, records)
			}
		})
	}
}
