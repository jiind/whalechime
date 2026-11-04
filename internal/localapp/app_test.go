package localapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jiind/whalechime/internal/dockerconn"
	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/notify"
)

// testApp는 외부 서비스와 사용자 설정을 건드리지 않는 임시 앱을 실행한다.
func testApp(t *testing.T) *App {
	t.Helper()
	a, err := Start(context.Background(), t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)), func(_ context.Context, _ notify.Message) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

// call은 실행별 로컬 자격 증명으로 JSON 요청을 보내 응답 상태를 확인한다.
func call(t *testing.T, a *App, method, path string, body any, status int) []byte {
	t.Helper()
	data, _ := json.Marshal(body)
	req, err := http.NewRequest(method, a.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err = io.ReadAll(response.Body)
	if err != nil || response.StatusCode != status {
		t.Fatalf("%s %s 상태=%d 기대=%d 본문=%s 오류=%v", method, path, response.StatusCode, status, data, err)
	}
	return data
}

// TestLocalSessionBoundary는 외부 호스트·사이트·인증 없는 로컬 요청을 거부한다.
func TestLocalSessionBoundary(t *testing.T) {
	a := testApp(t)
	for _, kind := range []string{"anonymous", "origin", "host", "form"} {
		t.Run(kind, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, a.URL+"/api/servers", strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+a.Token)
			req.Header.Set("Content-Type", "application/json")
			want := http.StatusForbidden
			switch kind {
			case "anonymous":
				req.Header.Del("Authorization")
				want = http.StatusUnauthorized
			case "origin":
				req.Header.Set("Origin", "https://outside.example")
			case "host":
				req.Host = "outside.example"
			case "form":
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				want = http.StatusUnsupportedMediaType
			}
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != want {
				t.Fatalf("보호 경계 상태: %d 기대 %d", response.StatusCode, want)
			}
		})
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Get(a.WebURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || len(response.Cookies()) != 1 || !response.Cookies()[0].HttpOnly || response.Cookies()[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("트레이의 로컬 진입 쿠키 경계를 확인하세요")
	}
}

// TestEncryptedRestartAndDuplicate는 자격 증명 비노출·원자적 저장·중복 제거·재시작을 확인한다.
func TestEncryptedRestartAndDuplicate(t *testing.T) {
	// 1. 비활성 서버와 수신처를 만들고 같은 이벤트를 두 번 기록한다.
	a := testApp(t)
	var s dockerconn.Server
	json.Unmarshal(call(t, a, "POST", "/api/servers", dockerconn.Server{Name: "테스트", Address: "http://127.0.0.1:1"}, 201), &s)
	secret := "secret-credential-never-return"
	config := map[string]any{"name": "Webhook", "kind": "webhook", "enabled": true, "config": map[string]string{"url": "https://example.com/private", "token": secret}}
	body := call(t, a, "POST", "/api/me/destinations", config, 201)
	if bytes.Contains(body, []byte(secret)) || bytes.Contains(body, []byte("private")) {
		t.Fatal("수신처 저장 응답에 비밀정보가 포함되었습니다")
	}
	call(t, a, "PUT", "/api/me/preferences", Preferences{DefaultEnabled: true, Items: []Preference{}}, 204)
	e := event.Event{Type: "container", Action: "start", ActorID: "abc", TimeNano: time.Now().UnixNano()}
	if err := a.record(s, e); err != nil {
		t.Fatal(err)
	}
	if err := a.record(s, e); err != nil {
		t.Fatal(err)
	}
	if data := a.store.snapshot(); len(data.Events) != 1 || len(data.Jobs) != 2 {
		t.Fatal("이벤트나 수신처 발송 대기가 중복되었습니다")
	}

	// 2. 디스크 암호문과 조회 응답에는 비밀값이 없어야 한다.
	for _, path := range []string{"/api/me/destinations", "/api/me/deliveries"} {
		if bytes.Contains(call(t, a, "GET", path, nil, 200), []byte(secret)) {
			t.Fatal("조회 응답에 비밀정보가 포함되었습니다")
		}
	}
	dir := a.store.dir
	data, err := os.ReadFile(filepath.Join(dir, "state.enc"))
	if err != nil || bytes.Contains(data, []byte(secret)) || json.Valid(data) {
		t.Fatal("로컬 상태가 암호화되지 않았습니다")
	}
	if _, err := openStore(dir); err == nil {
		t.Fatal("같은 데이터의 중복 실행을 허용했습니다")
	}
	a.Close()

	// 3. 키와 암호화 파일을 함께 재사용하면 연결·이력·수신처를 복원한다.
	reopened, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.lock.Close()
	restored := reopened.snapshot()
	if len(restored.Servers) != 1 || len(restored.Events) != 1 || restored.Destinations[1].Config.Token != secret {
		t.Fatal("로컬 설정과 이력을 복원하지 못했습니다")
	}
	before := len(restored.Servers)
	if err := reopened.update(func(next *state) error {
		next.Servers = nil
		return io.ErrClosedPipe
	}); err == nil || len(reopened.snapshot().Servers) != before {
		t.Fatal("실패한 변경이 메모리에 반영되었습니다")
	}
}

// TestDirectEventsNotificationsAndLogs는 직접 연결 수집·발송·로그와 서버 해제를 검증한다.
func TestDirectEventsNotificationsAndLogs(t *testing.T) {
	// 1. 실제 HTTP 이벤트 스트림·로그 형식을 제공하는 읽기 전용 Docker를 준비한다.
	events := make(chan event.Event, 4)
	logCanceled := make(chan struct{}, 1)
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("API-Version", "1.56")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			t.Error("Docker 변경 요청이 전송되었습니다")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/events"):
			w.WriteHeader(200)
			http.NewResponseController(w).Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case e := <-events:
					json.NewEncoder(w).Encode(map[string]any{"Type": e.Type, "Action": e.Action, "Actor": map[string]any{"ID": e.ActorID}, "timeNano": e.TimeNano})
					http.NewResponseController(w).Flush()
				}
			}
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			io.WriteString(w, `[{"Id":"abc","Names":["/테스트"],"State":"running"}]`)
		case strings.HasSuffix(r.URL.Path, "/abc/json"):
			io.WriteString(w, `{"Id":"abc","Config":{"Tty":false}}`)
		case strings.HasSuffix(r.URL.Path, "/abc/logs"):
			if r.URL.Query().Get("tail") != "200" || r.URL.Query().Get("follow") != "1" || r.URL.Query().Get("timestamps") != "1" {
				t.Error("최근·실시간·시각 조회 옵션이 잘못되었습니다")
			}
			line := []byte("2026-11-04T00:00:00Z 정상 출력\n")
			header := make([]byte, 8)
			header[0] = 1
			binary.BigEndian.PutUint32(header[4:], uint32(len(line)))
			w.Write(append(header, line...))
			http.NewResponseController(w).Flush()
			<-r.Context().Done()
			logCanceled <- struct{}{}
		}
	}))
	defer docker.Close()
	t.Setenv("WHALECHIME_ALLOWED_NOTIFICATION_HOSTS", "127.0.0.1")
	a := testApp(t)
	defer a.Close()

	// 2. 감시 서버·개인 항목·수신처를 서로 독립적으로 설정한다.
	var s dockerconn.Server
	json.Unmarshal(call(t, a, "POST", "/api/servers", dockerconn.Server{Name: "직접 연결", Address: docker.URL, Enabled: true}, 201), &s)
	var count atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(204)
	}))
	defer sink.Close()
	call(t, a, "POST", "/api/me/destinations", map[string]any{"name": "로컬 수신처", "kind": "webhook", "enabled": true, "config": map[string]string{"url": sink.URL}}, 201)
	call(t, a, "PUT", "/api/me/preferences", Preferences{Items: []Preference{{EventKey: "container.start", Enabled: true}}}, 204)
	events <- event.Event{Type: "container", Action: "start", ActorID: "abc", TimeNano: time.Now().UnixNano()}
	deadline := time.Now().Add(5 * time.Second)
	for count.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if count.Load() != 1 || len(a.store.snapshot().Events) != 1 {
		t.Fatal("직접 이벤트 수집과 개인 발송이 완료되지 않았습니다")
	}
	call(t, a, "GET", "/api/servers/"+s.ID+"/containers", nil, 200)

	// 3. 실제 로그 프레임을 읽고 연결 삭제 시 Docker 스트림도 즉시 닫는다.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", a.URL+"/api/servers/"+s.ID+"/containers/abc/logs", nil)
	req.Header.Set("Authorization", "Bearer "+a.Token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)
	for {
		var frame dockerconn.Frame
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == "error" {
			t.Fatal(frame.Error)
		}
		if strings.Contains(string(frame.Data), "정상 출력") {
			break
		}
	}
	call(t, a, "DELETE", "/api/servers/"+s.ID, nil, 204)
	select {
	case <-logCanceled:
	case <-ctx.Done():
		t.Fatal("서버 연결 삭제 후 로그 조회가 남았습니다")
	}
}

// TestRetryAndRevokedDestination는 실패 수신처 재시도와 대기 중 해제를 검증한다.
func TestRetryAndRevokedDestination(t *testing.T) {
	// 1. 첫 요청만 실패하는 로컬 수신처와 활성 서버를 준비한다.
	t.Setenv("WHALECHIME_ALLOWED_NOTIFICATION_HOSTS", "127.0.0.1")
	a := testApp(t)
	var attempts atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer sink.Close()
	var s dockerconn.Server
	json.Unmarshal(call(t, a, "POST", "/api/servers", dockerconn.Server{Name: "서버", Address: "http://127.0.0.1:1", Enabled: true}, 201), &s)
	var d Destination
	json.Unmarshal(call(t, a, "POST", "/api/me/destinations", map[string]any{"name": "재시도", "kind": "webhook", "enabled": true, "config": map[string]string{"url": sink.URL}}, 201), &d)
	call(t, a, "PUT", "/api/me/preferences", Preferences{DefaultEnabled: true, Items: []Preference{}}, 204)
	e := event.Event{Type: "container", Action: "start", ActorID: "abc", TimeNano: time.Now().UnixNano()}
	if err := a.record(s, e); err != nil {
		t.Fatal(err)
	}

	// 2. 성공한 PC 알림을 반복하지 않고 실패한 Webhook만 재시도한다.
	deadline := time.Now().Add(6 * time.Second)
	sent := false
	for !sent && time.Now().Before(deadline) {
		for _, j := range a.store.snapshot().Jobs {
			if j.DestinationID == d.ID && j.Status == "sent" && j.SentAt != nil {
				sent = true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !sent || attempts.Load() != 2 {
		t.Fatal("실패한 수신처만 재시도하고 성공 시각을 저장하지 못했습니다")
	}

	// 3. 아직 전송하지 않은 작업은 수신처를 끄면 취소한다.
	if err := a.store.update(func(data *state) error {
		for i := range data.Destinations {
			if data.Destinations[i].ID == d.ID {
				data.Destinations[i].Enabled = false
			}
		}
		data.Jobs = append(data.Jobs, job{Delivery: Delivery{ID: "revoked", DestinationID: d.ID, Status: "pending"}, Source: Record{ServerID: s.ID, EventKey: e.Key(), Payload: e}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, j := range a.store.snapshot().Jobs {
			if j.ID == "revoked" && j.Status == "cancelled" {
				if attempts.Load() != 2 {
					t.Fatal("해제한 수신처에 전송했습니다")
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("대기 중 해제된 수신처를 취소하지 못했습니다")
}
