package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/inspection"
	"github.com/moby/moby/client"
)

// Agent는 Docker 수집과 중앙 서버 업로드를 서로 독립적으로 실행한다.
type Agent struct {
	Docker *client.Client
	Spool  *Spool
	URL    string
	Token  string
	HTTP   *http.Client
	Logger *slog.Logger
}

// New는 수집기와 제한 시간이 있는 중앙 서버 연결을 구성한다.
func New(serverURL, token, dataDir string, logger *slog.Logger) (*Agent, error) {
	// 1. 중앙 서버 주소와 필수 토큰을 검증한다.
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("WHALECHIME_SERVER_URL을 확인하세요")
	}
	if token == "" {
		return nil, errors.New("WHALECHIME_AGENT_TOKEN이 필요합니다")
	}

	// 2. 공식 SDK와 영속 스풀을 준비한다.
	docker, err := client.New(client.FromEnv, client.WithUserAgent("whalechime-agent/0.1.0"))
	if err != nil {
		return nil, err
	}
	spool, err := OpenSpool(dataDir)
	if err != nil {
		docker.Close()
		return nil, err
	}
	return &Agent{Docker: docker, Spool: spool, URL: strings.TrimRight(serverURL, "/"), Token: token, Logger: logger,
		HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: rejectRedirect}}, nil
}

// rejectRedirect는 서버 토큰이 다른 서버로 전송되는 것을 막는다.
func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

// Close는 Docker 연결과 스풀 잠금을 해제한다.
func (a *Agent) Close() {
	a.Docker.Close()
	_ = a.Spool.Close()
}

// Run은 수집과 업로드를 병행하고 로컬 저장 장애가 발생하면 종료한다.
func (a *Agent) Run(ctx context.Context) error {
	// 1. 업로드와 생존 신호가 수집 스트림을 막지 않도록 분리한다.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() {
		results <- a.collect(ctx)
	}()
	go func() {
		results <- a.upload(ctx)
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.heartbeat(ctx)
	}()
	inspectionDone := make(chan struct{})
	go func() {
		defer close(inspectionDone)
		inspection.RunAgent(ctx, a.Docker, a.URL, a.Token)
	}()

	// 2. 한 처리의 치명적 실패 시 나머지를 취소하고 종료를 기다린다.
	err := <-results
	cancel()
	<-results
	<-done
	<-inspectionDone
	return err
}

// collect는 필터 없이 모든 Docker 이벤트를 읽고 연결 끊김 시 재접속한다.
func (a *Agent) collect(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		// 1. 마지막 저장 커서부터 제한 없는 이벤트 스트림을 연다.
		streamCtx, cancel := context.WithCancel(ctx)
		stream := a.Docker.Events(streamCtx, client.EventsListOptions{Since: a.Spool.Since()})
		connected := true
		for connected {
			select {
			case <-ctx.Done():
				cancel()
				return nil
			case <-stream.Err:
				connected = false
			case raw := <-stream.Messages:
				e := event.Event{Type: string(raw.Type), Action: string(raw.Action), ActorID: raw.Actor.ID, Scope: raw.Scope, TimeNano: raw.TimeNano, Attributes: raw.Actor.Attributes}
				if e.TimeNano == 0 {
					e.TimeNano = raw.Time * int64(time.Second)
				}
				if err := e.Validate(); err != nil {
					cancel()
					return err
				}
				if err := a.Spool.Append(e); err != nil {
					cancel()
					return fmt.Errorf("이벤트를 디스크에 저장할 수 없습니다: %w", err)
				}
				backoff = time.Second
			}
		}

		// 2. 과도한 재접속을 피하고 정상 이벤트가 오면 대기 시간을 초기화한다.
		cancel()
		if ctx.Err() == nil {
			a.Logger.Warn("Docker 이벤트 연결이 끊겨 재접속합니다")
		}
		if !sleep(ctx, backoff) {
			return nil
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	return nil
}

// upload는 DB 저장 확인을 받은 이벤트만 로컬에서 제거한다.
func (a *Agent) upload(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		// 1. 스풀을 읽는 동안에는 Docker 수집과 짧은 잠금만 공유한다.
		e, err := a.Spool.Pending()
		if errors.Is(err, os.ErrNotExist) {
			if !sleep(ctx, 200*time.Millisecond) {
				return nil
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("미전송 이벤트를 읽을 수 없습니다: %w", err)
		}

		// 2. 일시적인 중앙 서버 장애는 디스크에 보관한 채 재시도한다.
		data, _ := json.Marshal(e)
		if err := a.post(ctx, "/api/agent/events", data); err != nil {
			if ctx.Err() == nil {
				a.Logger.Warn("중앙 서버 전송을 재시도합니다", "error", err)
			}
			if !sleep(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		if err := a.Spool.Acknowledge(e); err != nil {
			return err
		}
		backoff = time.Second
	}
	return nil
}

// heartbeat는 이벤트 발생 여부와 무관하게 중앙 서버에 생존 신호를 보낸다.
func (a *Agent) heartbeat(ctx context.Context) {
	for ctx.Err() == nil {
		if err := a.post(ctx, "/api/agent/heartbeat", []byte("{}")); err != nil && ctx.Err() == nil {
			a.Logger.Warn("생존 신호 전송 실패", "error", err)
		}
		if !sleep(ctx, 30*time.Second) {
			return
		}
	}
}

// post는 서버 토큰으로 요청하고 응답 코드만 오류에 포함한다.
func (a *Agent) post(ctx context.Context, path string, body []byte) error {
	// 1. 인증 헤더를 쿼리나 로그에 노출하지 않고 전송한다.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("중앙 서버 요청 생성 실패")
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := a.HTTP.Do(req)
	if err != nil {
		return errors.New("중앙 서버 연결 실패")
	}
	defer response.Body.Close()

	// 2. HTTP 실패는 스풀에서 제거하지 않도록 호출자에게 반환한다.
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("중앙 서버 HTTP %d", response.StatusCode)
	}
	return nil
}

// sleep은 종료 요청을 받으면 재접속 대기를 중단한다.
func sleep(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
