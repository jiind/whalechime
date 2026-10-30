package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jiind/whalechime/internal/agent"
	"github.com/moby/moby/client"
)

// main은 에이전트 설정을 읽고 종료 신호까지 이벤트 수집을 실행한다.
func main() {
	// 1. 필수 연결 정보와 영속 데이터 디렉터리를 준비한다.
	check := flag.Bool("check", false, "Docker 연결과 데이터 디렉터리 접근만 확인하고 종료")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	token, err := agentToken()
	if err != nil {
		logger.Error("에이전트 설정 확인 실패", "error", err)
		os.Exit(1)
	}
	dataDir := os.Getenv("WHALECHIME_AGENT_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data/agent"
	}
	a, err := agent.New(os.Getenv("WHALECHIME_SERVER_URL"), token, dataDir, logger)
	if err != nil {
		logger.Error("에이전트 초기화 실패", "error", err)
		os.Exit(1)
	}
	if *check {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := a.Docker.Ping(ctx, client.PingOptions{})
		cancel()
		a.Close()
		if err != nil {
			logger.Error("Docker 접근 확인 실패: 소켓 경로와 실행 계정·그룹을 확인하세요")
			os.Exit(1)
		}
		logger.Info("Docker 소켓과 에이전트 데이터 디렉터리 접근 확인 완료")
		return
	}

	// 2. 정상 종료와 치명적 저장 장애를 구분해 자원을 정리한다.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err = a.Run(ctx)
	stop()
	a.Close()
	if err != nil {
		logger.Error("에이전트 종료", "error", err)
		os.Exit(1)
	}
}
