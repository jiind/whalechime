package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/notify"
	"github.com/jiind/whalechime/internal/store"
)

// TestTeamAccess는 팀·직접 연결 중복 제거와 권한 해제 후 발송 취소를 검증한다.
func TestTeamAccess(t *testing.T) {
	// 1. 관리자와 일반 사용자, 서버를 실제 DB에 등록한다.
	db := testStore(t)
	ctx := context.Background()
	adminToken := credential.NewToken("admin_")
	if err := db.BootstrapAdmin(ctx, adminToken); err != nil {
		t.Fatal(err)
	}
	user, token, err := db.CreateUser(ctx, "운영자")
	if err != nil {
		t.Fatal(err)
	}
	server, _, err := db.CreateServer(ctx, "team-server", "bootstrap-admin")
	if err != nil {
		t.Fatal(err)
	}
	cipher, _ := credential.NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	handler := (&API{Store: db, Cipher: cipher, Sender: notify.NewSender(""), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()
	call(t, handler, "POST", "/api/admin/teams", token, map[string]string{"name": "권한 없음"}, 403)
	var team store.Team
	data := call(t, handler, "POST", "/api/admin/teams", adminToken, map[string]string{"name": "운영팀"}, 201)
	if err := json.Unmarshal(data, &team); err != nil {
		t.Fatal(err)
	}

	// 2. 팀만 연결한 사용자도 서버 조회와 개인 알림 수신이 가능해야 한다.
	call(t, handler, "PUT", "/api/admin/teams/"+team.ID+"/members/"+user.ID, adminToken, map[string]bool{"enabled": true}, 204)
	call(t, handler, "PUT", "/api/admin/servers/"+server.ID+"/teams/"+team.ID, adminToken, map[string]bool{"enabled": true}, 204)
	items, err := db.ListServers(ctx, user.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("팀을 통한 서버 접근 실패: %v %v", items, err)
	}
	if err := db.SetPreferences(ctx, user.ID, store.Preferences{DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDestination(ctx, user.ID, store.Destination{ID: "team-dest", Name: "알림", Kind: "webhook", Enabled: true, EncryptedConfig: []byte("test")}, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMember(ctx, server.ID, user.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Ingest(ctx, server.ID, event.Event{Type: "image", Action: "push", TimeNano: time.Now().UnixNano()}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM deliveries").Scan(&count); err != nil || count != 1 {
		t.Fatalf("팀과 직접 연결이 중복 발송을 만들었습니다: %d %v", count, err)
	}

	// 3. 직접 연결과 팀 연결이 모두 해제되면 대기 중인 발송도 허용하지 않는다.
	if err := db.SetMember(ctx, server.ID, user.ID, false); err != nil {
		t.Fatal(err)
	}
	call(t, handler, "DELETE", "/api/admin/teams/"+team.ID, adminToken, nil, 204)
	job, err := db.ClaimDelivery(ctx)
	if err != nil || job.Allowed {
		t.Fatalf("팀 삭제 후 대기 중인 발송이 허용되었습니다: %v", err)
	}
	items, err = db.ListServers(ctx, user.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("팀 삭제 후 서버 접근이 남았습니다: %v %v", items, err)
	}
}
