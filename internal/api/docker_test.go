package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiind/whalechime/internal/agent"
	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/inspection"
	"github.com/jiind/whalechime/internal/notify"
	"github.com/jiind/whalechime/internal/store"
)

// TestDockerToNotification는 실제 Docker에서 수집한 이벤트가 개인 수신처까지 전달되는지 확인한다.
func TestDockerToNotification(t *testing.T) {
	if os.Getenv("WHALECHIME_TEST_DOCKER") != "1" {
		t.Skip("테스트 전용 Docker와 레지스트리를 명시한 경우에만 실행")
	}
	// 1. 실제 DB·중앙 API·에이전트·개인 Webhook을 연결한다.
	db := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.BootstrapAdmin(ctx, "test-admin"); err != nil {
		t.Fatal(err)
	}
	name := "whalechime-acceptance-" + strings.ToLower(credential.NewToken("")[:10])
	server, token, err := db.CreateServer(ctx, name, "bootstrap-admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPreferences(ctx, "bootstrap-admin", store.Preferences{DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var messages []string
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		messages = append(messages, string(data))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	cipher, _ := credential.NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	config, _ := json.Marshal(notify.Config{URL: sink.URL})
	if err := db.SaveDestination(ctx, "bootstrap-admin", store.Destination{ID: "real-docker-webhook", Name: "실제 Docker 검증", Kind: "webhook", Enabled: true, EncryptedConfig: cipher.Seal(config, "real-docker-webhook")}, true); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sender := notify.NewSender("127.0.0.1")
	central := httptest.NewServer(inspection.New(db).Wrap((&API{Store: db, Cipher: cipher, Sender: sender, Logger: logger}).Handler()))
	defer central.Close()
	a, err := agent.New(central.URL, token, t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx)
	}()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		(&notify.Worker{Store: db, Cipher: cipher, Sender: sender, Logger: logger}).Run(ctx)
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		<-workerDone
	}()

	// 2. 테스트가 만든 컨테이너와 이미지 태그만 변경해 실제 이벤트를 발생시킨다.
	var lastSeen *time.Time
	for lastSeen == nil {
		if err := db.Pool.QueryRow(ctx, "SELECT last_seen_at FROM servers WHERE id=$1", server.ID).Scan(&lastSeen); err != nil {
			t.Fatal(err)
		}
		if !testPause(ctx, 20*time.Millisecond) {
			t.Fatal("에이전트 연결 시간 초과")
		}
	}
	dockerCommand(t, ctx, "create", "--name", name, "alpine:3.22", "true")
	defer exec.Command("docker", "rm", "-f", name).Run()
	expected := []string{"container.create"}
	if registry := os.Getenv("WHALECHIME_TEST_REGISTRY"); registry != "" {
		tag := registry + "/" + name + ":test"
		dockerCommand(t, ctx, "tag", "alpine:3.22", tag)
		defer exec.Command("docker", "image", "rm", tag).Run()
		dockerCommand(t, ctx, "push", tag)
		dockerCommand(t, ctx, "pull", tag)
		expected = append(expected, "image.push", "image.pull")
	}

	// 3. 같은 이벤트가 중앙 기록과 실제 HTTP 수신처 양쪽에 도착해야 통과한다.
	for {
		items, err := db.ListEvents(ctx, "bootstrap-admin", server.ID, "", 500)
		if err != nil {
			t.Fatal(err)
		}
		seen := make(map[string]bool)
		for _, item := range items {
			payload, _ := json.Marshal(item.Payload)
			if strings.Contains(string(payload), name) {
				seen[item.EventKey] = true
			}
		}
		mu.Lock()
		joined := strings.Join(messages, "\n")
		mu.Unlock()
		complete := true
		for _, key := range expected {
			complete = complete && seen[key] && strings.Contains(joined, key)
		}
		if complete {
			t.Logf("실제 Docker → 에이전트 → PostgreSQL → 개인 Webhook 확인: %v", expected)
			return
		}
		if !testPause(ctx, 50*time.Millisecond) {
			t.Fatalf("실제 Docker 이벤트 전달 시간 초과: 관찰=%v 기대=%v", seen, expected)
		}
	}
}

// dockerCommand는 전용 검증에서만 테스트 소유 자원을 만드는 CLI를 호출한다.
func dockerCommand(t *testing.T, ctx context.Context, args ...string) {
	t.Helper()
	if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("테스트 Docker 명령 실패: %v %s", err, output)
	}
}

// testPause는 외부 시스템의 실제 상태 변화를 제한 시간 안에 기다린다.
func testPause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
