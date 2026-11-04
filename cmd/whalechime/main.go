// Whalechime의 로컬 웹 화면을 트레이 없이 실행하는 개발·진단용 진입점이다.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jiind/whalechime/internal/localapp"
)

// main은 사용자 설정 디렉터리에서 로컬 앱을 실행하고 종료 신호를 기다린다.
func main() {
	// 1. 사용자 전용 저장 경로와 선택적인 진단 세션 파일을 받는다.
	dir, err := localapp.DefaultDirectory()
	if err != nil {
		slog.Error("사용자 설정 경로를 확인할 수 없습니다")
		os.Exit(1)
	}
	dataDir := flag.String("data-dir", dir, "설정과 이력 저장 경로")
	sessionFile := flag.String("session-file", "", "개발·테스트용 로컬 진입 주소를 비공개 파일에 저장")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	app, err := localapp.Start(ctx, *dataDir, slog.Default())
	if err != nil {
		slog.Error("로컬 앱을 시작할 수 없습니다", "error", err)
		os.Exit(1)
	}
	defer app.Close()

	// 2. 세션 주소는 로그로 노출하지 않고 명시한 파일에만 기록한다.
	if *sessionFile != "" {
		data, _ := json.Marshal(map[string]string{"url": app.URL, "web_url": app.WebURL})
		file, err := os.OpenFile(*sessionFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			slog.Error("진단 세션 파일을 만들 수 없습니다")
			return
		}
		_, err = file.Write(data)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			os.Remove(*sessionFile)
			slog.Error("진단 세션 파일을 쓸 수 없습니다")
			return
		}
		defer os.Remove(*sessionFile)
	}
	slog.Info("로컬 앱 시작", "address", app.URL)
	<-ctx.Done()
}
