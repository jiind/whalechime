//go:build !windows

package dockerconn

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// TestSSHProcess는 테스트에서 만든 SSH 표준 입출력 경계만 처리한다.
func TestSSHProcess(t *testing.T) {
	if os.Getenv("WHALECHIME_SSH_HELPER") != "1" {
		return
	}
	// 1. 호스트 검증과 고정된 원격 명령 인자가 그대로 전달되는지 확인한다.
	args := strings.Join(os.Args, "|")
	if !strings.Contains(args, "StrictHostKeyChecking=yes") || !strings.HasSuffix(args, "--|test-alias|docker|system|dial-stdio") {
		os.Exit(2)
	}
	reader := bufio.NewReader(os.Stdin)

	// 2. 연결 수명 동안 Docker ping 응답을 실제 HTTP 형식으로 전송한다.
	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			os.Exit(0)
		}
		req.Body.Close()
		fmt.Fprint(os.Stdout, "HTTP/1.1 200 OK\r\nAPI-Version: 1.56\r\nContent-Length: 0\r\n\r\n")
	}
}

// TestSSHTransport는 셸 해석 없이 전달된 SSH 표준 입출력으로 SDK 요청을 운반한다.
func TestSSHTransport(t *testing.T) {
	// 1. 사용자 SSH 설정을 건드리지 않는 테스트 전용 프로세스 실행기를 만든다.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=TestSSHProcess -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WHALECHIME_SSH_HELPER", "1")
	docker, err := New(Server{Name: "SSH", Address: "ssh://test-user@test-alias:2222"})
	if err != nil {
		t.Fatal(err)
	}
	defer docker.Close()

	// 2. 실제 자식 프로세스의 양방향 입출력으로 HTTP 응답을 받는다.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := docker.Ping(ctx, client.PingOptions{}); err != nil {
		t.Fatal("SSH 표준 입출력 연결 실패:", err)
	}
}
