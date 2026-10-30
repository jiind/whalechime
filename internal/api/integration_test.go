package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/notify"
	"github.com/jiind/whalechime/internal/store"
)

// testStore는 전용 스키마를 만들어 실제 PostgreSQL 통합 테스트를 격리한다.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	// 1. 명시적으로 지정한 테스트 DB에만 임시 스키마를 생성한다.
	databaseURL := os.Getenv("WHALECHIME_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("WHALECHIME_TEST_DATABASE_URL이 없어 PostgreSQL 통합 테스트를 생략합니다")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schemaName := "test_" + strings.ToLower(credential.NewToken(""))
	schemaName = strings.ReplaceAll(schemaName, "-", "_")
	quoted := pgx.Identifier{schemaName}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()

	// 2. 동일한 실제 마이그레이션을 적용하고 테스트 종료 시에만 제거한다.
	db, err := store.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Pool.Close()
		_, _ = pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		pool.Close()
	})
	return db
}

// call은 실제 HTTP 핸들러를 호출하며 응답에서 테스트 계약을 확인한다.
func call(t *testing.T, handler http.Handler, method, path, token string, body any, status int) []byte {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != status {
		t.Fatalf("%s %s: 기대=%d 실제=%d 본문=%s", method, path, status, response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}

// TestPersonalNotificationFlow는 수집·개인 설정·권한·재시도를 실제 DB와 HTTP로 검증한다.
func TestPersonalNotificationFlow(t *testing.T) {
	// 1. 서로 다른 사용자와 한 서버를 구성하고 관련 사용자만 연결한다.
	db := testStore(t)
	ctx := context.Background()
	adminToken := credential.NewToken("test-admin-")
	if err := db.BootstrapAdmin(ctx, adminToken); err != nil {
		t.Fatal(err)
	}
	alice, aliceToken, err := db.CreateUser(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, bobToken, err := db.CreateUser(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	server, agentToken, err := db.CreateServer(ctx, "production", "bootstrap-admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetMember(ctx, server.ID, alice.ID, true); err != nil {
		t.Fatal(err)
	}
	cipher, _ := credential.NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sender := notify.NewSender("127.0.0.1")
	handler := (&API{Store: db, Cipher: cipher, Sender: sender, Logger: logger}).Handler()
	call(t, handler, "GET", "/api/me", agentToken, nil, 401)
	call(t, handler, "POST", "/api/agent/events", aliceToken, nil, 401)
	call(t, handler, "POST", "/api/admin/users", aliceToken, map[string]string{"name": "unauthorized"}, 403)

	// 2. 항목과 독립적으로 두 수신처를 등록하고 조회에서 비밀값을 숨긴다.
	var first, second atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a" {
			first.Add(1)
		} else if second.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	var destination store.Destination
	for _, suffix := range []string{"a", "b"} {
		body := map[string]any{"name": suffix, "kind": "webhook", "enabled": true, "config": map[string]string{"url": sink.URL + "/" + suffix, "token": "private-secret"}}
		data := call(t, handler, "POST", "/api/me/destinations", aliceToken, body, 201)
		if err := json.Unmarshal(data, &destination); err != nil {
			t.Fatal(err)
		}
	}
	data := call(t, handler, "GET", "/api/me/destinations", aliceToken, nil, 200)
	if strings.Contains(string(data), "private-secret") || strings.Contains(string(data), sink.URL) {
		t.Fatal("수신처 조회에 자격 증명이 노출되었습니다")
	}
	call(t, handler, "PATCH", "/api/me/destinations/"+destination.ID, bobToken, map[string]bool{"enabled": false}, 404)
	call(t, handler, "PUT", "/api/me/destinations/"+destination.ID, bobToken, map[string]any{"name": "다른 사용자 수정", "kind": "webhook", "enabled": true, "config": map[string]string{"url": sink.URL}}, 404)
	call(t, handler, "DELETE", "/api/me/destinations/"+destination.ID, bobToken, nil, 404)
	call(t, handler, "POST", "/api/me/destinations/"+destination.ID+"/test", bobToken, nil, 404)
	call(t, handler, "PUT", "/api/me/preferences", aliceToken, store.Preferences{Items: []store.Preference{{EventKey: "image.push", Enabled: true}}}, 204)
	if err := db.SetPreferences(ctx, bob.ID, store.Preferences{DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}

	// 3. 꺼둔 항목과 알 수 없는 이벤트도 기록하고 중복 발송 대기는 만들지 않는다.
	push := event.Event{Type: "image", Action: "push", ActorID: "app:v1", TimeNano: time.Now().UnixNano()}
	for _, e := range []event.Event{push, push, {Type: "image", Action: "pull", TimeNano: push.TimeNano + 1}, {Type: "future", Action: "new_action", TimeNano: push.TimeNano + 2}} {
		call(t, handler, "POST", "/api/agent/events", agentToken, e, 202)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM events").Scan(&count); err != nil || count != 3 {
		t.Fatalf("이벤트 수집 또는 중복 제거 실패: %d, %v", count, err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM deliveries").Scan(&count); err != nil || count != 2 {
		t.Fatalf("개인 설정에 따른 전체 수신처 선택 실패: %d, %v", count, err)
	}
	if body := call(t, handler, "GET", "/api/events", bobToken, nil, 200); string(bytes.TrimSpace(body)) != "[]" {
		t.Fatal("관련 없는 서버의 이벤트가 노출되었습니다")
	}
	if body := call(t, handler, "GET", "/api/event-types", aliceToken, nil, 200); !strings.Contains(string(body), "future.new_action") {
		t.Fatal("관찰한 새 이벤트가 수신 항목에 추가되지 않았습니다")
	}

	// 4. 실패한 수신처만 재시도하고 성공한 수신처는 다시 보내지 않는다.
	workerCtx, cancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		(&notify.Worker{Store: db, Cipher: cipher, Sender: sender, Logger: logger}).Run(workerCtx)
	}()
	defer func() {
		cancel()
		<-workerDone
	}()
	deadline := time.Now().Add(8 * time.Second)
	for {
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM deliveries WHERE status='sent'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("발송 작업이 완료되지 않았습니다")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if first.Load() != 1 || second.Load() != 2 {
		t.Fatalf("수신처별 재시도 실패: a=%d, b=%d", first.Load(), second.Load())
	}
}

// TestDeliveryLeaseAndSettings는 임대 재획득과 발송 직전 수신 해제를 검증한다.
func TestDeliveryLeaseAndSettings(t *testing.T) {
	// 1. 발송 대기를 생성하고 원래 작업자의 임대를 만료시킨다.
	db := testStore(t)
	ctx := context.Background()
	if err := db.BootstrapAdmin(ctx, "test-admin"); err != nil {
		t.Fatal(err)
	}
	server, _, err := db.CreateServer(ctx, "server", "bootstrap-admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPreferences(ctx, "bootstrap-admin", store.Preferences{DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDestination(ctx, "bootstrap-admin", store.Destination{ID: "dest", Name: "dest", Kind: "webhook", Enabled: true, EncryptedConfig: []byte("test")}, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Ingest(ctx, server.ID, event.Event{Type: "image", Action: "push", TimeNano: time.Now().UnixNano()}); err != nil {
		t.Fatal(err)
	}
	first, err := db.ClaimDelivery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE deliveries SET lease_until=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPreferences(ctx, "bootstrap-admin", store.Preferences{}); err != nil {
		t.Fatal(err)
	}

	// 2. 새 작업자는 최신 설정을 확인하고 이전 작업자는 결과를 덮어쓰지 못한다.
	second, err := db.ClaimDelivery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.LeaseToken == first.LeaseToken || second.Allowed {
		t.Fatal("임대 재발급 또는 최신 수신 설정 확인 실패")
	}
	if err := db.FinishDelivery(ctx, first, "sent", "", 0); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.Pool.QueryRow(ctx, "SELECT status FROM deliveries").Scan(&status); err != nil || status != "processing" {
		t.Fatal("만료된 임대가 새 작업을 덮어썼습니다")
	}
	if err := db.FinishDelivery(ctx, second, "cancelled", "수신 해제", 0); err != nil {
		t.Fatal(err)
	}
}
