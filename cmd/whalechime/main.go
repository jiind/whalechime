package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jiind/whalechime/internal/api"
	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/inspection"
	"github.com/jiind/whalechime/internal/notify"
	"github.com/jiind/whalechime/internal/store"
)

// main은 준비 상태 확인 또는 중앙 서버 실행 결과를 종료 코드로 반영한다.
func main() {
	// 1. 컨테이너 준비 상태 확인은 전체 서버 초기화 없이 수행한다.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	healthURL := flag.String("healthcheck", "", "준비 상태 확인 URL을 조회하고 종료")
	flag.Parse()
	if *healthURL != "" {
		if err := healthcheck(*healthURL); err != nil {
			logger.Error("준비 상태 확인 실패")
			os.Exit(1)
		}
		return
	}

	// 2. 일반 실행에서는 중앙 서버의 종료 결과를 반영한다.
	if err := run(logger); err != nil {
		logger.Error("중앙 서버 종료", "error", err)
		os.Exit(1)
	}
}

// healthcheck는 별도 셸 도구 없이 컨테이너 준비 상태를 확인한다.
func healthcheck(address string) error {
	// 1. 제한 시간 안에 준비 상태 URL에 연결한다.
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(address)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// 2. 성공 상태 코드가 아니면 준비되지 않은 것으로 처리한다.
	if response.StatusCode != http.StatusOK {
		return errors.New("중앙 서버가 준비되지 않았습니다")
	}
	return nil
}

// run은 DB·발송 작업자·HTTP 서버를 시작하고 종료 시 작업을 정리한다.
func run(logger *slog.Logger) error {
	// 1. 필수 설정과 DB를 확인한다.
	token := os.Getenv("WHALECHIME_ADMIN_TOKEN")
	if len(token) < 32 {
		return errors.New("WHALECHIME_ADMIN_TOKEN은 32바이트 이상이어야 합니다")
	}
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("DATABASE_URL이 필요합니다")
	}
	cipher, err := credential.NewCipher(os.Getenv("WHALECHIME_ENCRYPTION_KEY"))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	startCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	db, err := store.Open(startCtx, os.Getenv("DATABASE_URL"))
	if err != nil {
		cancel()
		return errors.New("PostgreSQL 연결 또는 스키마 초기화 실패")
	}
	defer db.Pool.Close()
	err = db.BootstrapAdmin(startCtx, token)
	cancel()
	if err != nil {
		return errors.New("관리자 계정 초기화 실패")
	}

	// 2. 발송 작업자와 HTTP 서버를 시작한다.
	sender := notify.NewSender(os.Getenv("WHALECHIME_ALLOWED_NOTIFICATION_HOSTS"))
	worker := &notify.Worker{Store: db, Cipher: cipher, Sender: sender, Logger: logger}
	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			worker.Run(workerCtx)
		})
	}
	address := os.Getenv("WHALECHIME_LISTEN_ADDR")
	if address == "" {
		address = ":8080"
	}
	handler := (&api.API{Store: db, Cipher: cipher, Sender: sender, Logger: logger}).Handler()
	server := &http.Server{
		Addr:              address,
		Handler:           inspection.New(db).Wrap(http.TimeoutHandler(handler, 30*time.Second, `{"error":"요청 처리 시간 초과"}`)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}
	result := make(chan error, 1)
	go func() {
		logger.Info("중앙 서버 시작", "address", address)
		result <- server.ListenAndServe()
	}()

	// 3. HTTP 요청을 정리하고 발송 작업자의 종료를 기다린다.
	select {
	case err = <-result:
	case <-ctx.Done():
	}
	stopWorkers()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	workers.Wait()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP 서버 실행 실패: %w", err)
	}
	return shutdownErr
}
