package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMenuInventoryAndDirectLogs는 서버별 목록과 선택 후 즉시 출력되는 화면을 검증한다.
func TestMenuInventoryAndDirectLogs(t *testing.T) {
	// 1. 서로 다른 두 Docker 서버를 표현하는 API에 연결한다.
	base, stop := startDemo()
	defer stop()
	c, err := newConnection(base, "preview-only")
	if err != nil {
		t.Fatal(err)
	}
	defer c.http.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	servers, err := c.inventory(ctx)
	if err != nil || len(servers) != 2 {
		t.Fatalf("서버 목록 실패: %v %v", servers, err)
	}
	if servers[0].Containers[0].Name == servers[1].Containers[0].Name {
		t.Fatal("서버별 컨테이너가 구분되지 않습니다")
	}
	v, local, listener, err := newViewer(c)
	if err != nil {
		t.Fatal(err)
	}
	go local.Serve(listener)
	defer stopServer(local)
	selected := target{ServerID: "preview-prod", ServerName: "<script>서버</script>", ContainerID: "payment-api", ContainerName: "payment-api"}
	address := v.open(selected)

	// 2. 폼이나 토큰 없이 선택 대상의 로그 화면을 직접 연다.
	response, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	html := string(data)
	if strings.Contains(html, "preview-only") || strings.Contains(html, "<script>서버") || strings.Contains(html, "<select") || strings.Contains(html, "<button") || strings.Contains(html, "<form") {
		t.Fatal("로그 화면에 토큰·실행 HTML·추가 선택 조작이 포함되었습니다")
	}
	if !strings.Contains(html, "&lt;script&gt;서버&lt;/script&gt;") {
		t.Fatal("서버 이름을 안전하게 표시하지 않았습니다")
	}
	icon, err := http.Get(v.base + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	icon.Body.Close()
	if icon.StatusCode != http.StatusNoContent {
		t.Fatal("브라우저 아이콘 요청에서 오류가 발생합니다")
	}
	response, err = http.Get(address + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)
	var frame struct {
		Kind string `json:"kind"`
		Data []byte `json:"data"`
	}
	if err := decoder.Decode(&frame); err != nil || frame.Kind != "ready" {
		t.Fatalf("로그 연결 실패: %v %v", frame, err)
	}
	if err := decoder.Decode(&frame); err != nil || !strings.Contains(string(frame.Data), "payment-api") {
		t.Fatalf("선택한 컨테이너 로그가 아님: %v %v", frame, err)
	}
}

// TestBrowserPreview는 명시적으로 요청한 브라우저 검증용 서버만 실행한다.
func TestBrowserPreview(t *testing.T) {
	// 1. 일반 테스트에서는 장시간 미리보기를 실행하지 않는다.
	path := os.Getenv("WHALECHIME_PREVIEW_ADDRESS_FILE")
	if path == "" {
		t.Skip("브라우저 검증 시에만 실행")
	}
	duration := 5 * time.Minute
	if value := os.Getenv("WHALECHIME_PREVIEW_DURATION"); value != "" {
		var err error
		duration, err = time.ParseDuration(value)
		if err != nil || duration <= 0 || duration > 5*time.Minute {
			t.Fatal("미리보기 유지 시간은 0초 초과 5분 이하로 지정하세요")
		}
	}
	base, stop := startDemo()
	defer stop()
	c, _ := newConnection(base, "preview-only")
	v, local, listener, err := newViewer(c)
	if err != nil {
		t.Fatal(err)
	}
	go local.Serve(listener)
	defer stopServer(local)

	// 2. 로컬 주소를 비공개 파일에 저장하고 검증 시간이 끝나면 정리한다.
	address := v.open(target{ServerID: "preview-prod", ServerName: "운영 서버 · 미리보기", ContainerID: "payment-api", ContainerName: "payment-api"})
	if err := os.WriteFile(path, []byte(address), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(duration)
}
