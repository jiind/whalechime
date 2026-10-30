package inspection

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jiind/whalechime/internal/store"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// TestRealTTYLogs는 실제 TTY 컨테이너의 합쳐진 출력과 한글을 원형대로 읽는지 검증한다.
func TestRealTTYLogs(t *testing.T) {
	// 1. 명시적으로 지정한 종료된 TTY 테스트 컨테이너만 조회한다.
	id := os.Getenv("WHALECHIME_TEST_TTY_CONTAINER_ID")
	if id == "" {
		t.Skip("실제 Docker TTY 로그 검증 시에만 실행")
	}
	docker, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatal(err)
	}
	defer docker.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var output bytes.Buffer
	streamLogs(ctx, docker, id, json.NewEncoder(&output))

	// 2. Docker의 TTY 출력에는 멀티플렉스 헤더가 없고 두 출력이 합쳐진다.
	decoder := json.NewDecoder(&output)
	var text strings.Builder
	for {
		var frame Frame
		err := decoder.Decode(&frame)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if frame.Kind == "error" {
			t.Fatal(frame.Error)
		}
		text.Write(frame.Data)
	}
	if !strings.Contains(text.String(), "whalechime-live-stdout 정상 출력") || !strings.Contains(text.String(), "whalechime-live-stderr 오류 출력") {
		t.Fatal("TTY 컨테이너의 stdout·stderr와 한글이 보존되지 않았습니다")
	}
}

type fixtureAuth struct{}

// AuthenticateUser는 테스트 사용자의 토큰만 허용한다.
func (fixtureAuth) AuthenticateUser(_ context.Context, token string) (store.User, error) {
	if token == "user-a" {
		return store.User{ID: "user-a"}, nil
	}
	return store.User{}, errors.New("인증 실패")
}

// TestRealDockerLogs는 명시한 테스트 컨테이너의 실제 Docker 출력을 읽기 전용으로 검증한다.
func TestRealDockerLogs(t *testing.T) {
	// 1. 실제 컨테이너를 지정한 검증에서만 Docker에 접속한다.
	id := os.Getenv("WHALECHIME_TEST_CONTAINER_ID")
	if id == "" {
		t.Skip("실제 Docker 로그 검증 시에만 실행")
	}
	docker, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatal(err)
	}
	defer docker.Close()
	central := httptest.NewServer(New(fixtureAuth{}).Wrap(http.NotFoundHandler()))
	defer central.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		RunAgent(ctx, docker, central.URL, "agent-a")
	}()
	defer func() {
		cancel()
		<-ended
	}()

	// 2. 사용자 요청을 중앙 서버와 에이전트를 거쳐 Docker에 전달한다.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, central.URL+"/api/servers/server-a/containers/"+id+"/logs", nil)
	req.Header.Set("Authorization", "Bearer user-a")
	response, err := central.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)
	seen := make(map[string]string)
	for !strings.Contains(seen["stdout"], "whalechime-live-stdout 정상 출력") || !strings.Contains(seen["stderr"], "whalechime-live-stderr 오류 출력") {
		var frame Frame
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == "error" {
			t.Fatal(frame.Error)
		}
		seen[frame.Stream] += string(frame.Data)
	}
	t.Log("실제 Docker의 선택 컨테이너에서 stdout·stderr와 한글 출력을 확인했습니다")
}

// AuthenticateAgent는 서로 다른 서버 에이전트를 구분한다.
func (fixtureAuth) AuthenticateAgent(_ context.Context, token string) (string, error) {
	if token == "agent-a" {
		return "server-a", nil
	}
	if token == "agent-b" {
		return "server-b", nil
	}
	return "", errors.New("인증 실패")
}

// ListServers는 사용자에게 연결된 서버 하나만 반환한다.
func (fixtureAuth) ListServers(_ context.Context, _ string) ([]store.Server, error) {
	return []store.Server{{ID: "server-a", Name: "운영"}}, nil
}

// TestAgentReadOnlyRoundTrip은 실제 SDK를 통한 목록·로그 중계와 접근 제한을 검증한다.
func TestAgentReadOnlyRoundTrip(t *testing.T) {
	// 1. 읽기 이외의 Docker 호출을 즉시 실패시키는 서버를 준비한다.
	var mutations atomic.Int32
	var logsEnded atomic.Bool
	dockerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations.Add(1)
			http.Error(w, "상태 변경 금지", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			fmt.Fprint(w, `[{"Id":"container-a","Names":["/payment-api"],"State":"running"}]`)
		case strings.HasSuffix(r.URL.Path, "/containers/container-a/json"):
			fmt.Fprint(w, `{"Id":"container-a","Config":{"Tty":false}}`)
		case strings.HasSuffix(r.URL.Path, "/containers/container-a/logs"):
			if r.URL.Query().Get("tail") != "200" || r.URL.Query().Get("follow") != "1" {
				t.Error("최근 로그와 실시간 출력 기본값이 누락되었습니다")
			}
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			writeDockerFrame(w, byte(stdcopy.Stdout), []byte("stdout 정상 출력\n"))
			writeDockerFrame(w, byte(stdcopy.Stderr), []byte("stderr 오류 출력\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			logsEnded.Store(true)
		default:
			http.NotFound(w, r)
		}
	}))
	defer dockerServer.Close()
	docker, err := client.New(client.WithHost(dockerServer.URL), client.WithAPIVersion("1.56"))
	if err != nil {
		t.Fatal(err)
	}
	defer docker.Close()
	broker := New(fixtureAuth{})
	central := httptest.NewServer(broker.Wrap(http.NotFoundHandler()))
	defer central.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		RunAgent(ctx, docker, central.URL, "agent-a")
	}()

	// 2. 서버 선택은 JSON 목록만 반환하고 컨테이너 선택 후에만 로그를 읽는다.
	get := func(path string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, central.URL+path, nil)
		req.Header.Set("Authorization", "Bearer user-a")
		response, err := central.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := get("/api/servers/server-a/containers")
	var containers []Container
	err = json.NewDecoder(response.Body).Decode(&containers)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || len(containers) != 1 || containers[0].Name != "payment-api" {
		t.Fatalf("컨테이너 목록 실패: status=%d items=%v err=%v", response.StatusCode, containers, err)
	}
	response = get("/api/servers/server-a/containers/container-a/logs")
	decoder := json.NewDecoder(response.Body)
	seen := map[string]string{}
	for len(seen) < 2 {
		var frame Frame
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == "log" {
			seen[frame.Stream] += string(frame.Data)
		}
		if frame.Kind == "error" {
			t.Fatal(frame.Error)
		}
	}
	response.Body.Close()
	if seen["stdout"] != "stdout 정상 출력\n" || seen["stderr"] != "stderr 오류 출력\n" {
		t.Fatalf("출력 스트림 손상: %v", seen)
	}
	response = get("/api/servers/server-b/containers")
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("관련 없는 서버에 접근할 수 있습니다")
	}

	// 3. 종료 시 Docker 로그 연결과 에이전트 작업을 함께 해제한다.
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("로그 중계 종료가 지연됩니다")
	}
	deadline := time.Now().Add(time.Second)
	for !logsEnded.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if mutations.Load() != 0 || !logsEnded.Load() {
		t.Fatalf("읽기 전용·취소 보장 실패: mutations=%d ended=%v", mutations.Load(), logsEnded.Load())
	}
}

// writeDockerFrame은 Docker가 사용하는 표준 멀티플렉스 형식으로 테스트 출력을 만든다.
func writeDockerFrame(w http.ResponseWriter, stream byte, data []byte) {
	var header [8]byte
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(data)))
	_, _ = w.Write(header[:])
	_, _ = w.Write(data)
}

// TestFramesRejectOtherAgent는 다른 서버 토큰으로 로그를 주입하지 못하게 한다.
func TestFramesRejectOtherAgent(t *testing.T) {
	// 1. 서버 A 작업을 등록한 상태를 준비한다.
	broker := New(fixtureAuth{})
	broker.requests["read-a"] = &request{task: Task{ID: "read-a"}, serverID: "server-a", claimed: true}
	handler := broker.Wrap(http.NotFoundHandler())

	// 2. 서버 B 응답을 보내면 원래 작업에 전달되지 않아야 한다.
	req := httptest.NewRequest(http.MethodPost, "/api/agent/inspection/tasks/read-a/frames", strings.NewReader(`{"kind":"log","data":"YWJj"}`+"\n"))
	req.Header.Set("Authorization", "Bearer agent-b")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusGone {
		t.Fatalf("다른 서버 응답이 허용됨: %d", response.Code)
	}
}
