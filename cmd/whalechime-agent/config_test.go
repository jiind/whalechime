package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentTokenSources는 비밀 파일 사용과 모호한 설정·비밀값 노출 방지를 검증한다.
func TestAgentTokenSources(t *testing.T) {
	// 1. 일반 환경변수와 비밀 파일을 각각 확인한다.
	t.Setenv("WHALECHIME_AGENT_TOKEN", "test-token")
	t.Setenv("WHALECHIME_AGENT_TOKEN_FILE", "")
	if value, err := agentToken(); err != nil || value != "test-token" {
		t.Fatal("환경변수 토큰을 읽지 못했습니다")
	}
	path := filepath.Join(t.TempDir(), "agent-token")
	if err := os.WriteFile(path, []byte("file-secret-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHALECHIME_AGENT_TOKEN", "")
	t.Setenv("WHALECHIME_AGENT_TOKEN_FILE", path)
	if value, err := agentToken(); err != nil || value != "file-secret-token" {
		t.Fatal("파일 토큰을 읽지 못했습니다")
	}

	// 2. 두 출처를 동시에 지정하거나 파일이 없으면 안전하게 실패한다.
	t.Setenv("WHALECHIME_AGENT_TOKEN", "another-secret")
	if _, err := agentToken(); err == nil || strings.Contains(err.Error(), "another-secret") {
		t.Fatal("중복 설정 검사 또는 비밀값 보호에 실패했습니다")
	}
	t.Setenv("WHALECHIME_AGENT_TOKEN", "")
	t.Setenv("WHALECHIME_AGENT_TOKEN_FILE", path+"-missing")
	if _, err := agentToken(); err == nil || strings.Contains(err.Error(), path) {
		t.Fatal("누락 파일 검사 또는 경로 비공개 처리에 실패했습니다")
	}
}
