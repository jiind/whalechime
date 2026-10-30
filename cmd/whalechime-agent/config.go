package main

import (
	"errors"
	"os"
	"strings"
)

// agentToken은 환경변수 또는 비밀 파일 한 곳에서만 서버 인증 토큰을 읽는다.
func agentToken() (string, error) {
	// 1. 서로 다른 토큰을 동시에 지정한 모호한 설정을 거부한다.
	value := os.Getenv("WHALECHIME_AGENT_TOKEN")
	path := os.Getenv("WHALECHIME_AGENT_TOKEN_FILE")
	if value != "" && path != "" {
		return "", errors.New("에이전트 토큰은 환경변수와 파일 중 한 곳에만 지정하세요")
	}

	// 2. Compose 비밀 파일을 사용하면 원문을 로그에 포함하지 않는다.
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", errors.New("에이전트 토큰 파일을 읽을 수 없습니다")
		}
		value = strings.TrimSpace(string(data))
	}
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("유효한 에이전트 토큰이 필요합니다")
	}
	return value, nil
}
