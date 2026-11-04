package localapp

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/dockerconn"
	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/notify"
	"github.com/moby/moby/client"
)

// App은 로컬 웹 화면과 서버별 수집·발송 작업의 수명을 함께 관리한다.
type App struct {
	URL        string
	Token      string
	WebURL     string
	store      *diskStore
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	settingsMu sync.Mutex
	runners    map[string]*runner
	work       sync.WaitGroup
	server     *http.Server
	sender     *notify.Sender
	logger     *slog.Logger
	once       sync.Once
	desktop    func(context.Context, notify.Message) error
}

// runner는 한 서버의 직접 연결과 연결 상태를 소유한다.
type runner struct {
	ctx    context.Context
	cancel context.CancelFunc
	client *client.Client
	status string
}

// DefaultDirectory는 운영체제별 사용자 설정 경로를 반환한다.
func DefaultDirectory() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "whalechime"), err
}

// Start는 PC 루프백에만 웹 화면을 열고 저장된 서버의 수집을 시작한다.
func Start(parent context.Context, dir string, logger *slog.Logger, notifier ...func(context.Context, notify.Message) error) (*App, error) {
	// 1. 파일 잠금·로컬 상태·루프백 연결을 준비한다.
	disk, err := openStore(dir)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		disk.lock.Close()
		return nil, errors.New("로컬 화면을 시작할 수 없습니다")
	}
	ctx, cancel := context.WithCancel(parent)
	a := &App{URL: "http://" + listener.Addr().String(), Token: credential.NewToken(""), store: disk, ctx: ctx, cancel: cancel, runners: map[string]*runner{}, sender: notify.NewSender(os.Getenv("WHALECHIME_ALLOWED_NOTIFICATION_HOSTS")), logger: logger}
	if len(notifier) > 0 {
		a.desktop = notifier[0]
	}
	a.WebURL = a.URL + "/open/" + a.Token
	a.server = &http.Server{Handler: a.handler(listener.Addr().String()), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(_ net.Listener) context.Context {
		return ctx
	}}

	// 2. 설정과 수집 이력을 유지하며 서버별 수집과 발송을 독립적으로 실행한다.
	a.work.Go(func() {
		if err := a.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("로컬 화면 처리 실패")
		}
	})
	for _, s := range disk.snapshot().Servers {
		a.startRunner(s)
	}
	a.work.Go(a.deliveries)
	return a, nil
}

// Close는 모든 스트림·SSH 프로세스·발송을 종료하고 파일 잠금을 해제한다.
func (a *App) Close() {
	a.once.Do(func() {
		// 1. 새 작업을 중단하고 진행 중인 요청을 취소한다.
		a.cancel()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.server.Shutdown(shutdown)
		stop()
		_ = a.server.Close()
		a.mu.Lock()
		for _, r := range a.runners {
			r.cancel()
			if r.client != nil {
				r.client.Close()
			}
		}
		a.mu.Unlock()

		// 2. 저장 중인 작업까지 종료한 뒤 다른 앱이 데이터를 열게 한다.
		a.work.Wait()
		a.store.lock.Close()
	})
}

// startRunner는 다른 서버의 장애와 독립적으로 해당 서버의 수집을 시작한다.
func (a *App) startRunner(s dockerconn.Server) {
	// 1. 이전 연결의 수집과 로그를 취소하고 설정을 교체한다.
	a.mu.Lock()
	defer a.mu.Unlock()
	if previous := a.runners[s.ID]; previous != nil {
		previous.cancel()
		if previous.client != nil {
			previous.client.Close()
		}
		delete(a.runners, s.ID)
	}
	if !s.Enabled || a.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	r := &runner{ctx: ctx, cancel: cancel, status: "연결 중"}
	a.runners[s.ID] = r

	// 2. 잘못된 인증서 파일도 서버 하나의 오류로 표시하고 나머지는 계속 수집한다.
	docker, err := dockerconn.New(s)
	if err != nil {
		r.status = "연결 설정 확인 필요"
		return
	}
	r.client = docker
	a.work.Go(func() {
		defer docker.Close()
		a.collect(ctx, s, r)
	})
}

// setStatus는 교체된 연결의 늦은 결과가 새 연결 상태를 덮어쓰지 못하게 한다.
func (a *App) setStatus(id string, r *runner, status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runners[id] == r {
		r.status = status
	}
}

// collect는 필터 없는 이벤트 스트림을 받고 끊어진 서버만 다시 연결한다.
func (a *App) collect(ctx context.Context, s dockerconn.Server, r *runner) {
	backoff := time.Second
	for ctx.Err() == nil {
		// 1. 디스크에 확정된 커서부터 이벤트를 재조회한다.
		cursor := a.store.snapshot().Cursors[s.ID]
		if cursor == 0 {
			cursor = time.Now().UnixNano()
			if err := a.store.update(func(data *state) error {
				if data.Cursors[s.ID] == 0 {
					data.Cursors[s.ID] = cursor
				}
				return nil
			}); err != nil {
				a.setStatus(s.ID, r, "로컬 저장 실패")
				if !pause(ctx, time.Second) {
					return
				}
				continue
			}
		}
		streamCtx, cancel := context.WithCancel(ctx)
		stream := r.client.Events(streamCtx, client.EventsListOptions{Since: time.Unix(0, cursor).UTC().Format(time.RFC3339Nano)})
		a.setStatus(s.ID, r, "대기 중")
		connected := true
		for connected && ctx.Err() == nil {
			select {
			case <-ctx.Done():
				connected = false
			case <-stream.Err:
				connected = false
			case raw, ok := <-stream.Messages:
				if !ok {
					connected = false
					break
				}
				e := event.Event{Type: string(raw.Type), Action: string(raw.Action), ActorID: raw.Actor.ID, Scope: raw.Scope, TimeNano: raw.TimeNano, Attributes: raw.Actor.Attributes}
				if e.TimeNano == 0 {
					e.TimeNano = raw.Time * int64(time.Second)
				}
				if e.Validate() != nil || a.record(s, e) != nil {
					connected = false
					a.logger.Error("이벤트를 로컬에 기록할 수 없습니다", "server_id", s.ID)
					break
				}
				a.setStatus(s.ID, r, "수집 중")
				backoff = time.Second
			}
		}

		// 2. 실패 내용에 인증정보를 포함하지 않고 제한된 간격으로 재접속한다.
		cancel()
		a.setStatus(s.ID, r, "연결 끊김 · 재시도 중")
		if !pause(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// record는 중복 이벤트를 제외하고 이력·커서·발송 대기를 함께 확정한다.
func (a *App) record(s dockerconn.Server, e event.Event) error {
	return a.store.update(func(data *state) error {
		// 1. 최근 이력에 같은 이벤트가 있으면 다시 발송하지 않는다.
		active := false
		for _, current := range data.Servers {
			active = active || (current.ID == s.ID && current.Address == s.Address && current.Enabled == s.Enabled)
		}
		if !active {
			return nil
		}
		id := e.ID(s.ID)
		for _, previous := range data.Events {
			if previous.ID == id {
				return nil
			}
		}
		if len(data.Jobs) >= 10000 {
			return errors.New("발송 대기 한도를 초과했습니다")
		}
		record := Record{ID: id, ServerID: s.ID, ServerName: s.Name, EventKey: e.Key(), OccurredAt: e.OccurredAt(), Payload: e}
		data.Events = append(data.Events, record)
		if len(data.Events) > 2000 {
			data.Events = data.Events[len(data.Events)-2000:]
		}
		data.Cursors[s.ID] = max(data.Cursors[s.ID], e.TimeNano)

		// 2. 수신 항목을 켠 이벤트만 모든 활성 수신처에 독립적으로 배분한다.
		if selected(data.Preferences, e.Key()) {
			for _, d := range data.Destinations {
				if d.Enabled {
					data.Jobs = append(data.Jobs, job{Delivery: Delivery{ID: credential.NewToken("job_"), DestinationID: d.ID, DestinationName: d.Name, EventID: id, Status: "pending", AvailableAt: time.Now()}, Source: record})
				}
			}
		}
		return nil
	})
}

// docker는 삭제·비활성화된 서버의 로그를 열지 않고 현재 연결만 반환한다.
func (a *App) docker(id string) (*client.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.runners[id]
	if r == nil || r.client == nil {
		return nil, errors.New("서버 연결을 확인하세요")
	}
	return r.client, nil
}

// deliveries는 최신 수신 설정을 확인하며 실패한 수신처만 최대 8회 시도한다.
func (a *App) deliveries() {
	for a.ctx.Err() == nil {
		// 1. 아직 발송할 작업과 현재 수신처를 디스크 상태에서 확인한다.
		data := a.store.snapshot()
		found := false
		for _, j := range data.Jobs {
			if j.Status != "pending" || time.Now().Before(j.AvailableAt) {
				continue
			}
			found = true
			var destination *Destination
			for _, d := range data.Destinations {
				if d.ID == j.DestinationID && d.Enabled {
					copy := d
					destination = &copy
				}
			}
			serverEnabled := false
			for _, s := range data.Servers {
				serverEnabled = serverEnabled || (s.ID == j.Source.ServerID && s.Enabled)
			}
			status, message := "cancelled", "수신 설정 또는 서버 연결이 해제되었습니다"
			attempts := j.Attempts
			if destination != nil && serverEnabled && selected(data.Preferences, j.Source.EventKey) {
				attempts++
				status, message = "sent", ""
				ctx, cancel := context.WithTimeout(a.ctx, 25*time.Second)
				e := j.Source.Payload
				err := a.send(ctx, destination, notify.Message{Title: "Whalechime · " + j.Source.ServerName + " · " + e.Key(), Body: "대상: " + e.ActorID + "\n이름: " + e.Attributes["name"] + "\n발생 시각: " + e.OccurredAt().Format(time.RFC3339Nano)})
				cancel()
				if err != nil {
					status, message = "pending", err.Error()
					if attempts >= 8 {
						status = "failed"
					}
				}
			}

			// 2. 결과를 저장하지 못한 작업은 다음 회차에 다시 확인한다.
			if err := a.store.update(func(next *state) error {
				for i := range next.Jobs {
					if next.Jobs[i].ID == j.ID {
						next.Jobs[i].Status = status
						next.Jobs[i].LastError = message
						next.Jobs[i].Attempts = attempts
						if status == "sent" {
							now := time.Now()
							next.Jobs[i].SentAt = &now
						}
						next.Jobs[i].AvailableAt = time.Now().Add(min(time.Duration(1<<attempts)*time.Second, 5*time.Minute))
					}
				}
				if len(next.Jobs) > 2000 {
					remaining := make([]job, 0, len(next.Jobs))
					completed := 0
					for i := len(next.Jobs) - 1; i >= 0; i-- {
						item := next.Jobs[i]
						if item.Status == "pending" || completed < 1000 {
							remaining = append(remaining, item)
							if item.Status != "pending" {
								completed++
							}
						}
					}
					for i, j := 0, len(remaining)-1; i < j; i, j = i+1, j-1 {
						remaining[i], remaining[j] = remaining[j], remaining[i]
					}
					next.Jobs = remaining
				}
				return nil
			}); err != nil {
				a.logger.Error("발송 결과를 저장할 수 없습니다")
				pause(a.ctx, time.Second)
			}
			break
		}
		if !found && !pause(a.ctx, 250*time.Millisecond) {
			return
		}
	}
}

// pause는 앱 종료 시 대기와 재접속을 즉시 취소한다.
func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// validName은 로컬 표시 이름의 크기와 제어문자를 검사한다.
func validName(value string) bool {
	return strings.TrimSpace(value) != "" && len([]rune(value)) <= 100 && !strings.ContainsAny(value, "\r\n")
}

// send는 PC 알림과 외부 수신처를 같은 재시도 흐름에서 발송한다.
func (a *App) send(ctx context.Context, d *Destination, m notify.Message) error {
	if d.Kind == "desktop" {
		if a.desktop == nil {
			return errors.New("PC 알림은 트레이 앱에서 사용할 수 있습니다")
		}
		return a.desktop(ctx, m)
	}
	return a.sender.Send(ctx, d.Kind, d.Config, m)
}
