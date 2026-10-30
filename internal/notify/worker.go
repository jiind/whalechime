package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/store"
)

// Worker는 DB 발송 대기를 처리하고 수신처별 재시도를 수행한다.
type Worker struct {
	Store  *store.Store
	Cipher *credential.Cipher
	Sender *Sender
	Logger *slog.Logger
}

// Run은 종료 요청까지 발송 작업을 반복해서 가져온다.
func (w *Worker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		// 1. 현재 처리할 작업을 하나 임대한다.
		job, err := w.Store.ClaimDelivery(ctx)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
				w.Logger.Error("발송 대기 조회 실패")
			}
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}

		// 2. 외부 전송과 상태 저장을 분리해 종료 중에도 결과를 기록한다.
		status, message, delay := w.deliver(ctx, job)
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = w.Store.FinishDelivery(finishCtx, job, status, message, delay)
		cancel()
		if err != nil {
			w.Logger.Error("발송 결과 저장 실패", "delivery_id", job.ID)
		}
	}
}

// deliver는 최신 수신 권한과 설정을 확인한 뒤 결과와 다음 재시도 시간을 계산한다.
func (w *Worker) deliver(ctx context.Context, job store.Delivery) (string, string, time.Duration) {
	// 1. 대기 중 해제된 설정이나 재시도 한도를 확인한다.
	if !job.Allowed {
		return "cancelled", "수신 설정 또는 서버 연결이 해제되었습니다", 0
	}
	if job.Attempts > 8 {
		return "failed", "발송 재시도 횟수를 초과했습니다", 0
	}
	data, err := w.Cipher.Open(job.Destination.EncryptedConfig, job.Destination.ID)
	if err != nil {
		return "failed", "수신처 설정을 복호화할 수 없습니다", 0
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return "failed", "수신처 설정이 올바르지 않습니다", 0
	}

	// 2. 제한 시간 안에 발송하고 실패한 수신처만 다시 대기시킨다.
	sendCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := w.Sender.Send(sendCtx, job.Destination.Kind, config, EventMessage(job.ServerName, job.Event)); err != nil {
		if job.Attempts >= 8 {
			return "failed", err.Error(), 0
		}
		return "pending", err.Error(), min(time.Duration(1<<job.Attempts)*time.Second, 5*time.Minute)
	}
	return "sent", "", 0
}

// EventMessage는 모든 채널에서 읽을 수 있는 간결한 이벤트 요약을 만든다.
func EventMessage(serverName string, e event.Event) Message {
	return Message{
		Title: "Whalechime · " + truncate(serverName, 100) + " · " + e.Key(),
		Body:  fmt.Sprintf("서버: %s\n이벤트: %s\n대상: %s\n이름: %s\n발생 시각: %s", truncate(serverName, 100), truncate(e.Action, 500), truncate(e.ActorID, 200), truncate(e.Attributes["name"], 200), e.OccurredAt().Format(time.RFC3339Nano)),
	}
}

// wait는 종료 신호를 받으면 재시도 대기를 즉시 중단한다.
func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
