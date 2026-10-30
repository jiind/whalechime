// Package inspection은 Docker 서버에 대한 읽기 전용 목록·로그 요청을 중계한다.
package inspection

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/store"
)

var validContainerID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,255}$`)

// Authority는 기존 사용자·에이전트 인증과 서버 연결 권한을 조회한다.
type Authority interface {
	AuthenticateUser(context.Context, string) (store.User, error)
	AuthenticateAgent(context.Context, string) (string, error)
	ListServers(context.Context, string) ([]store.Server, error)
}

// Container는 트레이 메뉴와 로그 창에 필요한 최소 컨테이너 정보다.
type Container struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// Task는 에이전트가 실행할 수 있는 두 종류의 읽기 전용 요청이다.
type Task struct {
	ID          string `json:"id"`
	Operation   string `json:"operation"`
	ContainerID string `json:"container_id,omitempty"`
}

// Frame은 컨테이너 목록, 로그 조각 또는 명시적인 조회 상태다.
type Frame struct {
	Kind       string      `json:"kind"`
	Containers []Container `json:"containers,omitempty"`
	Data       []byte      `json:"data,omitempty"`
	Stream     string      `json:"stream,omitempty"`
	Error      string      `json:"error,omitempty"`
}

type request struct {
	task     Task
	serverID string
	ctx      context.Context
	frames   chan Frame
	claimed  bool
}

// Broker는 에이전트의 외부 연결만으로 컨테이너 조회와 로그를 전달한다.
type Broker struct {
	auth     Authority
	mu       sync.Mutex
	requests map[string]*request
	notify   chan struct{}
}

// New는 다른 중앙 기능과 분리된 읽기 전용 중계기를 만든다.
func New(auth Authority) *Broker {
	return &Broker{auth: auth, requests: make(map[string]*request), notify: make(chan struct{}, 1)}
}

// Wrap은 일반 API의 제한 시간과 별도로 로그 스트림 경로를 등록한다.
func (b *Broker) Wrap(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/servers/{server}/containers", b.containers)
	mux.HandleFunc("GET /api/servers/{server}/containers/{container}/logs", b.logs)
	mux.HandleFunc("GET /api/agent/inspection/tasks", b.poll)
	mux.HandleFunc("POST /api/agent/inspection/tasks/{task}/frames", b.frames)
	mux.Handle("/", next)
	return mux
}

// token은 요청의 Bearer 자격 증명만 추출한다.
func token(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

// permitted는 토큰 소유자와 서버 연결 권한을 함께 확인한다.
func (b *Broker) permitted(ctx context.Context, user store.User, serverID string) bool {
	servers, err := b.auth.ListServers(ctx, user.ID)
	if err != nil {
		return false
	}
	for _, server := range servers {
		if server.ID == serverID {
			return true
		}
	}
	return false
}

// begin은 사용자 권한을 확인하고 제한된 개수의 조회 작업을 등록한다.
func (b *Broker) begin(w http.ResponseWriter, r *http.Request, operation string) (*request, store.User, func()) {
	// 1. 메뉴 항목에 보였더라도 실제 조회 시 서버 권한을 다시 확인한다.
	if operation == "logs" && !validContainerID.MatchString(r.PathValue("container")) {
		http.Error(w, "컨테이너 식별자가 올바르지 않습니다", http.StatusBadRequest)
		return nil, store.User{}, nil
	}
	user, err := b.auth.AuthenticateUser(r.Context(), token(r))
	if err != nil {
		http.Error(w, "인증이 필요합니다", http.StatusUnauthorized)
		return nil, user, nil
	}
	serverID := r.PathValue("server")
	if !b.permitted(r.Context(), user, serverID) {
		http.Error(w, "서버를 조회할 수 없습니다", http.StatusNotFound)
		return nil, user, nil
	}

	// 2. 브라우저 연결 수명에 묶인 작업을 등록하고 한도를 적용한다.
	ctx, cancel := context.WithCancel(r.Context())
	job := &request{task: Task{ID: credential.NewToken("read_"), Operation: operation, ContainerID: r.PathValue("container")}, serverID: serverID, ctx: ctx, frames: make(chan Frame, 16)}
	b.mu.Lock()
	if len(b.requests) >= 128 {
		b.mu.Unlock()
		cancel()
		http.Error(w, "동시 로그 조회 한도입니다", http.StatusServiceUnavailable)
		return nil, user, nil
	}
	b.requests[job.task.ID] = job
	b.mu.Unlock()
	select {
	case b.notify <- struct{}{}:
	default:
	}
	return job, user, func() {
		cancel()
		b.mu.Lock()
		delete(b.requests, job.task.ID)
		b.mu.Unlock()
	}
}

// containers는 에이전트가 실제 Docker에서 조회한 컨테이너 목록만 반환한다.
func (b *Broker) containers(w http.ResponseWriter, r *http.Request) {
	// 1. 관련 서버의 에이전트에 읽기 전용 요청을 등록한다.
	job, _, done := b.begin(w, r, "containers")
	if job == nil {
		return
	}
	defer done()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	// 2. 응답이 없을 때 빈 목록 대신 연결 오류를 반환한다.
	select {
	case <-r.Context().Done():
	case <-timer.C:
		http.Error(w, "서버 에이전트가 응답하지 않습니다", http.StatusGatewayTimeout)
	case frame := <-job.frames:
		if frame.Kind != "containers" {
			http.Error(w, "컨테이너 목록을 조회할 수 없습니다", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(frame.Containers)
	}
}

// logs는 선택한 컨테이너의 로그를 즉시 스트리밍하고 권한 해제를 반영한다.
func (b *Broker) logs(w http.ResponseWriter, r *http.Request) {
	// 1. 일반 API 응답 제한에서 분리한 스트림을 연다.
	job, user, done := b.begin(w, r, "logs")
	if job == nil {
		return
	}
	defer done()
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	encoder := json.NewEncoder(w)
	_ = encoder.Encode(Frame{Kind: "connecting"})
	_ = controller.Flush()
	first := time.NewTimer(10 * time.Second)
	defer first.Stop()
	permissions := time.NewTicker(10 * time.Second)
	defer permissions.Stop()

	// 2. 버퍼를 제한한 상태로 로그를 전달하고 연결과 권한을 확인한다.
	for {
		select {
		case <-job.ctx.Done():
			return
		case <-first.C:
			_ = encoder.Encode(Frame{Kind: "error", Error: "서버 에이전트가 응답하지 않습니다"})
			_ = controller.Flush()
			return
		case <-permissions.C:
			if !b.permitted(r.Context(), user, job.serverID) {
				_ = encoder.Encode(Frame{Kind: "error", Error: "서버 조회 권한이 해제되었습니다"})
				_ = controller.Flush()
				return
			}
		case frame := <-job.frames:
			first.Stop()
			_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if err := encoder.Encode(frame); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
			if frame.Kind == "done" || frame.Kind == "error" {
				return
			}
		}
	}
}

// claim은 인증된 서버 소유 작업 하나만 원자적으로 가져온다.
func (b *Broker) claim(serverID string) *request {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, job := range b.requests {
		if job.serverID == serverID && !job.claimed && job.ctx.Err() == nil {
			job.claimed = true
			return job
		}
	}
	return nil
}

// poll은 에이전트가 먼저 연 연결에서 해당 서버의 읽기 요청을 전달한다.
func (b *Broker) poll(w http.ResponseWriter, r *http.Request) {
	// 1. 작업 수신은 서버 전용 토큰으로만 허용한다.
	serverID, err := b.auth.AuthenticateAgent(r.Context(), token(r))
	if err != nil {
		http.Error(w, "인증이 필요합니다", http.StatusUnauthorized)
		return
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()

	// 2. 다른 서버용 알림이 소비되어도 주기적으로 자기 작업을 확인한다.
	for {
		if job := b.claim(serverID); job != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(job.task)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-b.notify:
		case <-tick.C:
		}
	}
}

// frames는 해당 서버 에이전트의 응답만 크기를 제한해 원래 요청자에게 보낸다.
func (b *Broker) frames(w http.ResponseWriter, r *http.Request) {
	// 1. 응답 대상과 에이전트 소유 서버가 일치하는지 확인한다.
	serverID, err := b.auth.AuthenticateAgent(r.Context(), token(r))
	if err != nil {
		http.Error(w, "인증이 필요합니다", http.StatusUnauthorized)
		return
	}
	b.mu.Lock()
	job := b.requests[r.PathValue("task")]
	b.mu.Unlock()
	if job == nil || job.serverID != serverID || !job.claimed {
		http.Error(w, "만료된 조회 요청입니다", http.StatusGone)
		return
	}

	// 2. 긴 로그 업로드에서도 조각별 제한 시간과 최대 크기를 적용한다.
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	_ = controller.EnableFullDuplex()
	scanner := bufio.NewScanner(r.Body)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	terminal := false
	for {
		_ = controller.SetReadDeadline(time.Now().Add(30 * time.Second))
		if !scanner.Scan() {
			break
		}
		var frame Frame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			http.Error(w, "로그 응답 형식 오류", http.StatusBadRequest)
			return
		}
		if frame.Kind == "done" || frame.Kind == "error" || frame.Kind == "containers" {
			terminal = true
		}
		select {
		case <-job.ctx.Done():
			w.WriteHeader(http.StatusGone)
			return
		case <-r.Context().Done():
			return
		case job.frames <- frame:
		}
	}
	if !terminal && !errors.Is(scanner.Err(), context.Canceled) {
		select {
		case job.frames <- Frame{Kind: "error", Error: "로그 중계 연결이 끊겼습니다"}:
		case <-job.ctx.Done():
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
