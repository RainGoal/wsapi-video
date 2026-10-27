package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wsapi-video/internal/config"
	"wsapi-video/internal/core"
	"wsapi-video/migrations"
)

func authMailDB(t *testing.T, applyMigrations bool) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for auth mail integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_auth_mail_" + strings.TrimPrefix(core.ID("s"), "s_")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
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
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if applyMigrations {
		if err = migrations.Apply(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func queueAuthMail(t *testing.T, service *Service, userID, purpose string) string {
	t.Helper()
	tx, err := service.db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var email string
	if err = tx.QueryRow(context.Background(), `SELECT email FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	token, err := service.queueToken(context.Background(), tx, userID, email, purpose)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return token
}

func readAdminMailDetail(t *testing.T, service *Service, userID string, secrets ...string) adminMailDelivery {
	t.Helper()
	request := httptest.NewRequest("GET", "/api/v1/admin/users/"+userID, nil)
	request.SetPathValue("id", userID)
	response := httptest.NewRecorder()
	service.adminUser(response, request)
	if response.Code != 200 {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}
	for _, secret := range append(secrets, "password_hash", "token_hash", "sensitive-smtp-auth-secret") {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("detail exposed secret %q", secret)
		}
	}
	var body struct {
		Data struct {
			Mail adminMailDelivery `json:"mail_delivery"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data.Mail
}

type localSMTPFixture struct {
	port      int
	reject    atomic.Bool
	delivered atomic.Int32
	messages  chan string
}

func newLocalSMTPFixture(t *testing.T) *localSMTPFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &localSMTPFixture{port: portNumber, messages: make(chan string, 10)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				if fixture.reject.Load() {
					_, _ = fmt.Fprint(connection, "554 sensitive-smtp-auth-secret\r\n")
					return
				}
				_, _ = fmt.Fprint(connection, "220 localhost test SMTP\r\n")
				reader := bufio.NewReader(connection)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					command := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(command, "EHLO "), strings.HasPrefix(command, "HELO "), strings.HasPrefix(command, "MAIL FROM:"), strings.HasPrefix(command, "RCPT TO:"):
						_, _ = fmt.Fprint(connection, "250 localhost\r\n")
					case command == "DATA":
						_, _ = fmt.Fprint(connection, "354 End with a dot\r\n")
						var message strings.Builder
						for {
							data, err := reader.ReadString('\n')
							if err != nil {
								return
							}
							if data == ".\r\n" {
								break
							}
							message.WriteString(data)
						}
						fixture.messages <- message.String()
						fixture.delivered.Add(1)
						_, _ = fmt.Fprint(connection, "250 queued\r\n")
					case command == "QUIT":
						_, _ = fmt.Fprint(connection, "221 bye\r\n")
						return
					default:
						_, _ = fmt.Fprint(connection, "500 unsupported\r\n")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done })
	return fixture
}

func TestAdminMailDeliveryStatesAndRequeue(t *testing.T) {
	db := authMailDB(t, true)
	service := New(db, config.Config{PublicURL: "http://studio.example.test"})
	if err := service.BootstrapAdmin(context.Background(), "mail@example.test", "mail-password-2026"); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := db.QueryRow(context.Background(), `SELECT id FROM users WHERE email='mail@example.test'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if detail := readAdminMailDetail(t, service, userID); detail.Status != "none" {
		t.Fatalf("unexpected empty mail state: %+v", detail)
	}
	firstToken := queueAuthMail(t, service, userID, "verify")
	if detail := readAdminMailDetail(t, service, userID, firstToken); detail.Status != "unconfigured" || detail.Purpose != "verify" || detail.Attempts != 0 {
		t.Fatalf("unconfigured detail: %+v", detail)
	}
	smtp := newLocalSMTPFixture(t)
	service.cfg.SMTPHost, service.cfg.SMTPPort, service.cfg.SMTPFrom = "127.0.0.1", smtp.port, "studio@example.test"
	if detail := readAdminMailDetail(t, service, userID, firstToken); detail.Status != "queued" {
		t.Fatalf("queue detail: %+v", detail)
	}
	smtp.reject.Store(true)
	service.deliverMail(context.Background())
	failed := readAdminMailDetail(t, service, userID, firstToken)
	if failed.Status != "failed" || failed.Attempts != 1 || failed.ErrorCode != "smtp_delivery_failed" || failed.NextAttemptAt == nil {
		t.Fatalf("failed detail: %+v", failed)
	}
	if failed.NextAttemptAt.Before(time.Now().Add(30 * time.Second)) {
		t.Fatal("SMTP failure did not schedule delayed retry")
	}
	secondToken := queueAuthMail(t, service, userID, "verify")
	var cancelled, cleared, used bool
	if err := db.QueryRow(context.Background(), `SELECT cancelled_at IS NOT NULL,body='' FROM auth_mail_outbox WHERE user_id=$1 AND cancelled_at IS NOT NULL`, userID).Scan(&cancelled, &cleared); err != nil || !cancelled || !cleared {
		t.Fatalf("stale message not cancelled and scrubbed: %v %v %v", cancelled, cleared, err)
	}
	if err := db.QueryRow(context.Background(), `SELECT used_at IS NOT NULL FROM auth_tokens WHERE token_hash=$1`, digest(firstToken)).Scan(&used); err != nil || !used {
		t.Fatalf("stale token still valid: %v %v", used, err)
	}
	if detail := readAdminMailDetail(t, service, userID, firstToken, secondToken); detail.Status != "queued" || detail.Attempts != 0 || detail.ErrorCode != "" {
		t.Fatalf("requeued detail: %+v", detail)
	}
	smtp.reject.Store(false)
	service.deliverMail(context.Background())
	sent := readAdminMailDetail(t, service, userID, firstToken, secondToken)
	if sent.Status != "sent" || sent.SentAt == nil || sent.NextAttemptAt != nil || sent.ErrorCode != "" {
		t.Fatalf("sent detail: %+v", sent)
	}
	if smtp.delivered.Load() != 1 {
		t.Fatalf("delivered %d messages, want one replacement", smtp.delivered.Load())
	}
	select {
	case message := <-smtp.messages:
		if !strings.Contains(message, secondToken) || strings.Contains(message, firstToken) {
			t.Fatal("SMTP did not deliver only the current verification link")
		}
	case <-time.After(time.Second):
		t.Fatal("missing delivered SMTP message")
	}
	var sentBody string
	if err := db.QueryRow(context.Background(), `SELECT body FROM auth_mail_outbox WHERE user_id=$1 AND sent_at IS NOT NULL`, userID).Scan(&sentBody); err != nil || sentBody != "" {
		t.Fatalf("sent ticket body retained: %v", err)
	}
}

func TestUserManagementMigrationBackfillsPendingMailPurposes(t *testing.T) {
	db := authMailDB(t, false)
	_, sourceFile, _, _ := runtime.Caller(0)
	migrationDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "migrations")
	initial, err := os.ReadFile(filepath.Join(migrationDir, "001_identity_projects_assets.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(context.Background(), string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(context.Background(), `INSERT INTO users(id,email,password_hash,display_name) VALUES('legacy-user','legacy@example.test','unused-hash','Legacy user'); INSERT INTO auth_mail_outbox(id,user_id,to_email,subject,body) VALUES('legacy-verify','legacy-user','legacy@example.test','验证您的创作平台邮箱','obsolete verify link'),('legacy-reset','legacy-user','legacy@example.test','重置您的创作平台密码','obsolete reset link')`); err != nil {
		t.Fatal(err)
	}
	update, err := os.ReadFile(filepath.Join(migrationDir, "009_user_management.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(context.Background(), string(update)); err != nil {
		t.Fatal(err)
	}
	service := New(db, config.Config{PublicURL: "http://studio.example.test"})
	queueAuthMail(t, service, "legacy-user", "verify")
	queueAuthMail(t, service, "legacy-user", "reset")
	var replaced int
	if err = db.QueryRow(context.Background(), `SELECT count(*) FROM auth_mail_outbox WHERE id IN ('legacy-verify','legacy-reset') AND purpose IN ('verify','reset') AND cancelled_at IS NOT NULL AND body=''`).Scan(&replaced); err != nil || replaced != 2 {
		t.Fatalf("legacy pending mail was not safely superseded: count=%d err=%v", replaced, err)
	}
	var current int
	if err = db.QueryRow(context.Background(), `SELECT count(*) FROM auth_mail_outbox WHERE user_id='legacy-user' AND cancelled_at IS NULL AND body<>''`).Scan(&current); err != nil || current != 2 {
		t.Fatalf("replacement mail missing: count=%d err=%v", current, err)
	}
}
