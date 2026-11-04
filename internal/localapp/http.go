package localapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/dockerconn"
	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/notify"
	"github.com/jiind/whalechime/internal/webui"
	"github.com/moby/moby/client"
)

// handler는 루프백 호스트와 실행별 자격 증명으로 로컬 설정 화면을 보호한다.
func (a *App) handler(host string) http.Handler {
	// 1. 트레이·설정 화면에서 필요한 로컬 기능만 등록한다.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/servers", a.servers)
	mux.HandleFunc("POST /api/servers", a.saveServer)
	mux.HandleFunc("PUT /api/servers/{id}", a.saveServer)
	mux.HandleFunc("DELETE /api/servers/{id}", a.deleteServer)
	mux.HandleFunc("POST /api/servers/{id}/test", a.testServer)
	mux.HandleFunc("GET /api/servers/{id}/containers", a.containers)
	mux.HandleFunc("GET /api/servers/{id}/containers/{container}/logs", a.logs)
	mux.HandleFunc("GET /api/me/preferences", a.preferences)
	mux.HandleFunc("PUT /api/me/preferences", a.preferences)
	mux.HandleFunc("GET /api/me/destinations", a.destinations)
	mux.HandleFunc("POST /api/me/destinations", a.saveDestination)
	mux.HandleFunc("PUT /api/me/destinations/{id}", a.saveDestination)
	mux.HandleFunc("PATCH /api/me/destinations/{id}", a.changeDestination)
	mux.HandleFunc("DELETE /api/me/destinations/{id}", a.changeDestination)
	mux.HandleFunc("POST /api/me/destinations/{id}/test", a.testDestination)
	mux.HandleFunc("GET /api/events", a.events)
	mux.HandleFunc("GET /api/me/deliveries", a.history)
	mux.HandleFunc("GET /api/event-types", a.eventTypes)
	mux.HandleFunc("GET /api/destination-types", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, append([]string{"desktop"}, notify.Kinds...))
	})
	mux.Handle("/", webui.Handler())

	// 2. 브라우저에는 HttpOnly 세션 쿠키만 주고 외부 사이트의 요청은 거부한다.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Host != host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != a.URL) {
			writeError(w, http.StatusForbidden, "로컬 앱에서만 접근할 수 있습니다")
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/open/"+a.Token {
			http.SetCookie(w, &http.Cookie{Name: "whalechime", Value: a.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		cookie, _ := r.Cookie("whalechime")
		if r.Header.Get("Authorization") != "Bearer "+a.Token && (cookie == nil || cookie.Value != a.Token) {
			writeError(w, http.StatusUnauthorized, "트레이의 웹으로 이동 또는 설정 메뉴에서 화면을 여세요")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "JSON 요청이 필요합니다")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// servers는 연결 설정과 상태만 반환하고 인증서·키의 원문은 읽지 않는다.
func (a *App) servers(w http.ResponseWriter, _ *http.Request) {
	type view struct {
		dockerconn.Server
		Status string `json:"status"`
	}
	items := []view{}
	data := a.store.snapshot()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range data.Servers {
		status := "감시 꺼짐"
		if r := a.runners[s.ID]; r != nil {
			status = r.status
		}
		items = append(items, view{Server: s, Status: status})
	}
	writeJSON(w, http.StatusOK, items)
}

// saveServer는 오프라인 서버도 저장하고 연결 변경 시 기존 스트림을 교체한다.
func (a *App) saveServer(w http.ResponseWriter, r *http.Request) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	// 1. 저장 전에 인증서 파일과 연결 설정 조합을 확인한다.
	var s dockerconn.Server
	if !readJSON(w, r, &s) {
		return
	}
	docker, err := dockerconn.New(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	docker.Close()
	s.ID = r.PathValue("id")
	create := r.Method == http.MethodPost
	if create {
		s.ID = credential.NewToken("server_")
	}
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}

	// 2. 원자적 저장이 성공한 경우에만 감시 작업을 새 설정으로 바꾼다.
	err = a.store.update(func(data *state) error {
		if create {
			if len(data.Servers) >= 100 {
				return errors.New("서버는 최대 100대까지 등록할 수 있습니다")
			}
			data.Servers = append(data.Servers, s)
			return nil
		}
		for i, previous := range data.Servers {
			if previous.ID == s.ID {
				if previous.Address != s.Address {
					delete(data.Cursors, s.ID)
				}
				data.Servers[i] = s
				return nil
			}
		}
		return errors.New("서버를 찾을 수 없습니다")
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.startRunner(s)
	writeJSON(w, status, s)
}

// deleteServer는 로컬 연결과 진행 중인 스트림을 제거하고 Docker 자체는 변경하지 않는다.
func (a *App) deleteServer(w http.ResponseWriter, r *http.Request) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	id := r.PathValue("id")
	err := a.store.update(func(data *state) error {
		for i, s := range data.Servers {
			if s.ID == id {
				data.Servers = append(data.Servers[:i], data.Servers[i+1:]...)
				delete(data.Cursors, id)
				return nil
			}
		}
		return errors.New("서버를 찾을 수 없습니다")
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.startRunner(dockerconn.Server{ID: id})
	w.WriteHeader(http.StatusNoContent)
}

// testServer는 저장된 연결로 Docker의 응답 여부만 확인한다.
func (a *App) testServer(w http.ResponseWriter, r *http.Request) {
	// 1. 감시를 끈 서버도 같은 인증 설정으로 시험할 수 있게 한다.
	for _, s := range a.store.snapshot().Servers {
		if s.ID != r.PathValue("id") {
			continue
		}
		docker, err := dockerconn.New(s)
		if err != nil {
			writeError(w, http.StatusBadGateway, "연결 설정을 확인하세요")
			return
		}
		defer docker.Close()

		// 2. 제한 시간 안에 조회하고 내부 오류나 인증정보는 반환하지 않는다.
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if _, err := docker.Ping(ctx, client.PingOptions{}); err != nil {
			writeError(w, http.StatusBadGateway, "서버에 연결할 수 없습니다. 주소·인증·접근 권한을 확인하세요")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusNotFound, "서버를 찾을 수 없습니다")
}

// containers는 활성 서버의 실제 컨테이너를 읽기 전용으로 조회한다.
func (a *App) containers(w http.ResponseWriter, r *http.Request) {
	// 1. 현재 연결에만 제한 시간이 있는 조회를 실행한다.
	docker, err := a.docker(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := docker.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		writeError(w, http.StatusBadGateway, "컨테이너 목록을 조회할 수 없습니다")
		return
	}

	// 2. 컨테이너의 이름과 상태만 메뉴로 전달한다.
	items := []dockerconn.Container{}
	for _, item := range result.Items {
		name := item.ID[:min(12, len(item.ID))]
		if len(item.Names) > 0 {
			name = strings.TrimPrefix(item.Names[0], "/")
		}
		items = append(items, dockerconn.Container{ID: item.ID, Name: name, State: string(item.State)})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	writeJSON(w, http.StatusOK, items)
}

// flushWriter는 로그 프레임마다 브라우저에 출력하고 느린 연결의 쓰기를 제한한다.
type flushWriter struct {
	w http.ResponseWriter
}

// Write는 하나의 JSON 프레임을 쓰고 즉시 전송한다.
func (f flushWriter) Write(data []byte) (int, error) {
	controller := http.NewResponseController(f.w)
	_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
	n, err := f.w.Write(data)
	if err == nil {
		err = controller.Flush()
	}
	return n, err
}

// logs는 선택된 컨테이너의 최근 200줄과 실시간 출력을 직접 전달한다.
func (a *App) logs(w http.ResponseWriter, r *http.Request) {
	// 1. 브라우저와 서버 연결 중 어느 쪽이 종료되어도 로그 조회를 취소한다.
	a.mu.Lock()
	connection := a.runners[r.PathValue("id")]
	if connection == nil || connection.client == nil {
		a.mu.Unlock()
		writeError(w, http.StatusBadGateway, "서버 연결을 확인하세요")
		return
	}
	docker, connectionCtx := connection.client, connection.ctx
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(connectionCtx, cancel)
	defer stop()

	// 2. 컨테이너를 조작하지 않고 최근 출력과 이후 로그만 전달한다.
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	dockerconn.StreamLogs(ctx, docker, r.PathValue("container"), json.NewEncoder(flushWriter{w: w}))
}

// preferences는 수신처와 별개인 이벤트 선택을 조회하거나 전체 교체한다.
func (a *App) preferences(w http.ResponseWriter, r *http.Request) {
	// 1. 조회는 저장된 설정을 그대로 반환한다.
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.store.snapshot().Preferences)
		return
	}
	var p Preferences
	if !readJSON(w, r, &p) {
		return
	}
	valid := regexp.MustCompile(`^[a-z][a-z0-9_-]{0,127}\.[a-z][a-z0-9_-]{0,127}$`)
	seen := map[string]bool{}
	if len(p.Items) > 1000 {
		writeError(w, http.StatusBadRequest, "수신 항목은 1000개까지 설정할 수 있습니다")
		return
	}
	for _, item := range p.Items {
		if !valid.MatchString(item.EventKey) || seen[item.EventKey] {
			writeError(w, http.StatusBadRequest, "수신 항목 이름과 중복 여부를 확인하세요")
			return
		}
		seen[item.EventKey] = true
	}

	// 2. 검증한 설정을 다른 연결·수신처와 독립적으로 저장한다.
	if err := a.store.update(func(data *state) error {
		data.Preferences = p
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "설정을 저장할 수 없습니다")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// destinations는 자격 증명 필드를 제외한 수신처 메타데이터만 반환한다.
func (a *App) destinations(w http.ResponseWriter, _ *http.Request) {
	items := []map[string]any{}
	for _, d := range a.store.snapshot().Destinations {
		items = append(items, map[string]any{"id": d.ID, "name": d.Name, "kind": d.Kind, "enabled": d.Enabled})
	}
	writeJSON(w, http.StatusOK, items)
}

// saveDestination은 연결 정보를 암호화된 로컬 상태에 저장한다.
func (a *App) saveDestination(w http.ResponseWriter, r *http.Request) {
	// 1. 연결 정보와 표시 이름을 검증한다.
	var d Destination
	if !readJSON(w, r, &d) {
		return
	}
	if !validName(d.Name) {
		writeError(w, http.StatusBadRequest, "수신처 이름을 확인하세요")
		return
	}
	if err := a.validateDestination(d); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	create := r.Method == http.MethodPost
	d.ID = r.PathValue("id")
	if create {
		d.ID = credential.NewToken("dest_")
	}

	// 2. 저장 후 응답에는 연결 URL·토큰·비밀번호를 포함하지 않는다.
	err := a.store.update(func(data *state) error {
		for _, existing := range data.Destinations {
			if existing.ID != d.ID && existing.Name == d.Name {
				return errors.New("같은 이름의 수신처가 있습니다")
			}
		}
		if create && len(data.Destinations) < 100 {
			data.Destinations = append(data.Destinations, d)
			return nil
		}
		for i := range data.Destinations {
			if data.Destinations[i].ID == d.ID {
				data.Destinations[i] = d
				return nil
			}
		}
		return errors.New("수신처 개수와 대상을 확인하세요")
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"id": d.ID, "name": d.Name, "kind": d.Kind, "enabled": d.Enabled})
}

// changeDestination은 로컬 수신처의 사용 여부를 변경하거나 삭제한다.
func (a *App) changeDestination(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if r.Method == http.MethodPatch && (!readJSON(w, r, &input) || input.Enabled == nil) {
		if input.Enabled == nil {
			writeError(w, http.StatusBadRequest, "enabled 값이 필요합니다")
		}
		return
	}
	err := a.store.update(func(data *state) error {
		for i := range data.Destinations {
			if data.Destinations[i].ID == r.PathValue("id") {
				if r.Method == http.MethodDelete {
					data.Destinations = append(data.Destinations[:i], data.Destinations[i+1:]...)
				} else {
					data.Destinations[i].Enabled = *input.Enabled
				}
				return nil
			}
		}
		return errors.New("수신처를 찾을 수 없습니다")
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testDestination은 사용자가 선택한 저장 수신처에만 테스트 메시지를 보낸다.
func (a *App) testDestination(w http.ResponseWriter, r *http.Request) {
	for _, d := range a.store.snapshot().Destinations {
		if d.ID == r.PathValue("id") {
			ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer cancel()
			if err := a.send(ctx, &d, notify.Message{Title: "Whalechime 연결 확인", Body: "이 수신처로 알림을 받을 수 있습니다."}); err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeError(w, http.StatusNotFound, "수신처를 찾을 수 없습니다")
}

// events는 최근 로컬 이력을 서버·항목별로 최대 500개 반환한다.
func (a *App) events(w http.ResponseWriter, r *http.Request) {
	items := []Record{}
	data := a.store.snapshot()
	for i := len(data.Events) - 1; i >= 0 && len(items) < 500; i-- {
		e := data.Events[i]
		if id := r.URL.Query().Get("server_id"); id != "" && id != e.ServerID {
			continue
		}
		if key := r.URL.Query().Get("event_key"); key != "" && key != e.EventKey {
			continue
		}
		items = append(items, e)
	}
	writeJSON(w, http.StatusOK, items)
}

// history는 비밀 설정과 발송 본문 없이 최근 발송 결과를 반환한다.
func (a *App) history(w http.ResponseWriter, _ *http.Request) {
	items := []Delivery{}
	data := a.store.snapshot()
	for i := len(data.Jobs) - 1; i >= 0 && len(items) < 500; i-- {
		items = append(items, data.Jobs[i].Delivery)
	}
	writeJSON(w, http.StatusOK, items)
}

// eventTypes는 알려진 항목과 실제 관찰한 이벤트를 합쳐 반환한다.
func (a *App) eventTypes(w http.ResponseWriter, _ *http.Request) {
	set := map[string]bool{}
	for _, key := range event.KnownKeys() {
		set[key] = true
	}
	for _, e := range a.store.snapshot().Events {
		set[e.EventKey] = true
	}
	keys := []string{}
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writeJSON(w, http.StatusOK, keys)
}

// readJSON은 크기·알 수 없는 필드·추가 값을 제한해 로컬 입력을 검증한다.
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		writeError(w, http.StatusBadRequest, "JSON 본문과 필드·크기를 확인하세요")
		return false
	}
	return true
}

// writeJSON은 공통 JSON 응답을 작성한다.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeError는 자격 증명 없는 사용자용 오류를 반환한다.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// validateDestination은 PC 알림에 불필요한 자격 증명을 받지 않는다.
func (a *App) validateDestination(d Destination) error {
	if d.Kind == "desktop" {
		if d.Config != (notify.Config{}) {
			return errors.New("PC 알림에는 연결 정보를 입력하지 않습니다")
		}
		return nil
	}
	return a.sender.Validate(d.Kind, d.Config)
}
