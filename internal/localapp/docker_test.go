package localapp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/dockerconn"
)

// TestRealDockerDirect는 테스트 전용 루프백 경로로 실제 Docker 이벤트와 로그를 확인한다.
func TestRealDockerDirect(t *testing.T) {
	if os.Getenv("WHALECHIME_TEST_DOCKER") != "1" {
		t.Skip("실제 Docker 검증을 명시한 경우에만 실행")
	}
	// 1. 데몬 설정을 바꾸지 않고 테스트 프로세스의 루프백에서만 소켓을 중계한다.
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	defer transport.CloseIdleConnections()
	target, _ := url.Parse("http://docker")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport
	proxy.FlushInterval = -1
	server := httptest.NewServer(proxy)
	defer server.Close()
	a := testApp(t)
	defer a.Close()
	var s dockerconn.Server
	json.Unmarshal(call(t, a, "POST", "/api/servers", dockerconn.Server{Name: "실제 Docker", Address: server.URL, Enabled: true}, 201), &s)

	// 2. 테스트 소유 컨테이너만 만들고 실제 생성 이벤트가 수집되는지 확인한다.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name := "whalechime-local-" + strings.ToLower(credential.NewToken("")[:10])
	output, err := exec.CommandContext(ctx, "docker", "run", "--detach", "--name", name, "alpine:3.22", "sh", "-c", "echo 'whalechime 실제 한글 로그'").CombinedOutput()
	if err != nil {
		t.Fatalf("테스트 컨테이너 생성 실패: %s %v", output, err)
	}
	defer exec.Command("docker", "rm", "-f", name).Run()
	id := strings.TrimSpace(string(output))
	seen := false
	for !seen && ctx.Err() == nil {
		for _, e := range a.store.snapshot().Events {
			seen = seen || (e.EventKey == "container.create" && e.Payload.ActorID == id)
		}
		if !seen {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !seen {
		t.Fatal("직접 연결에서 실제 Docker 생성 이벤트를 수집하지 못했습니다")
	}

	// 3. 같은 연결로 실제 한글 컨테이너 로그를 자동 조회한다.
	req, _ := http.NewRequestWithContext(ctx, "GET", a.URL+"/api/servers/"+s.ID+"/containers/"+id+"/logs", nil)
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
			t.Fatal("실제 로그 조회 실패:", err)
		}
		if frame.Kind == "error" {
			t.Fatal(frame.Error)
		}
		if strings.Contains(string(frame.Data), "whalechime 실제 한글 로그") {
			t.Log("실제 Docker 직접 연결 → 이벤트 수집 → 한글 로그 조회 통과")
			return
		}
	}
}
