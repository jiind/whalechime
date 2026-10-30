package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/notify"
	"github.com/jiind/whalechime/internal/store"
	"github.com/jiind/whalechime/internal/webui"
)

// API는 사용자 인증과 HTTP 계약을 담당한다.
type API struct {
	Store  *store.Store
	Cipher *credential.Cipher
	Sender *notify.Sender
	Logger *slog.Logger
}

type userHandler func(http.ResponseWriter, *http.Request, store.User)

// Handler는 사용자 API와 서버 전용 수집 API를 분리해 등록한다.
func (a *API) Handler() http.Handler {
	// 1. 인증 없이 확인할 생존·준비 상태를 등록한다.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /readyz", a.ready)

	// 2. 개인 설정과 관리자 기능에 사용자 인증을 적용한다.
	mux.HandleFunc("GET /api/me", a.user(a.me, false))
	mux.HandleFunc("GET /api/me/preferences", a.user(a.getPreferences, false))
	mux.HandleFunc("PUT /api/me/preferences", a.user(a.putPreferences, false))
	mux.HandleFunc("GET /api/me/destinations", a.user(a.listDestinations, false))
	mux.HandleFunc("POST /api/me/destinations", a.user(a.saveDestination, false))
	mux.HandleFunc("PUT /api/me/destinations/{id}", a.user(a.saveDestination, false))
	mux.HandleFunc("PATCH /api/me/destinations/{id}", a.user(a.enableDestination, false))
	mux.HandleFunc("DELETE /api/me/destinations/{id}", a.user(a.deleteDestination, false))
	mux.HandleFunc("POST /api/me/destinations/{id}/test", a.user(a.testDestination, false))
	mux.HandleFunc("GET /api/me/deliveries", a.user(a.deliveries, false))
	mux.HandleFunc("GET /api/event-types", a.user(a.eventTypes, false))
	mux.HandleFunc("GET /api/destination-types", a.user(a.destinationTypes, false))
	mux.HandleFunc("GET /api/servers", a.user(a.servers, false))
	mux.HandleFunc("GET /api/events", a.user(a.events, false))
	mux.HandleFunc("POST /api/admin/users", a.user(a.createUser, true))
	mux.HandleFunc("GET /api/admin/users", a.user(a.adminUsers, true))
	mux.HandleFunc("POST /api/admin/users/{user}/token", a.user(a.rotateToken, true))
	mux.HandleFunc("GET /api/admin/servers", a.user(a.adminServers, true))
	mux.HandleFunc("POST /api/admin/servers", a.user(a.createServer, true))
	mux.HandleFunc("GET /api/admin/servers/{server}/members", a.user(a.members, true))
	mux.HandleFunc("PUT /api/admin/servers/{server}/members/{user}", a.user(a.setMember, true))
	mux.HandleFunc("GET /api/admin/teams", a.user(a.teams, true))
	mux.HandleFunc("POST /api/admin/teams", a.user(a.createTeam, true))
	mux.HandleFunc("DELETE /api/admin/teams/{team}", a.user(a.deleteTeam, true))
	mux.HandleFunc("GET /api/admin/teams/{team}/members", a.user(a.teamMembers, true))
	mux.HandleFunc("PUT /api/admin/teams/{team}/members/{user}", a.user(a.setTeamLink, true))
	mux.HandleFunc("GET /api/admin/servers/{server}/teams", a.user(a.serverTeams, true))
	mux.HandleFunc("PUT /api/admin/servers/{server}/teams/{team}", a.user(a.setTeamLink, true))

	// 3. 서버 토큰으로만 이벤트와 생존 신호를 받는다.
	mux.HandleFunc("POST /api/agent/events", a.ingest)
	mux.HandleFunc("POST /api/agent/heartbeat", a.heartbeat)
	mux.Handle("/", webui.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

// enableDestination은 본인 수신처의 활성 상태만 변경한다.
func (a *API) enableDestination(w http.ResponseWriter, r *http.Request, user store.User) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled 값이 필요합니다")
		return
	}
	if err := a.Store.EnableDestination(r.Context(), user.ID, r.PathValue("id"), *input.Enabled); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminUsers는 관리자가 서버 연결에 사용할 사용자 목록을 반환한다.
func (a *API) adminUsers(w http.ResponseWriter, r *http.Request, _ store.User) {
	items, err := a.Store.ListUsers(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// adminServers는 관리자가 등록된 전체 서버를 조회하게 한다.
func (a *API) adminServers(w http.ResponseWriter, r *http.Request, _ store.User) {
	items, err := a.Store.ListAllServers(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// members는 관리자에게 특정 서버에 연결된 사용자 목록을 반환한다.
func (a *API) members(w http.ResponseWriter, r *http.Request, _ store.User) {
	items, err := a.Store.ListMembers(r.Context(), r.PathValue("server"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// rotateToken은 관리자가 일반 사용자의 분실된 토큰을 재발급하게 한다.
func (a *API) rotateToken(w http.ResponseWriter, r *http.Request, _ store.User) {
	token, err := a.Store.RotateUserToken(r.Context(), r.PathValue("user"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// health는 프로세스가 HTTP 요청을 처리할 수 있는지 반환한다.
func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ready는 제한 시간 안에 PostgreSQL 연결을 확인한다.
func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.Store.Pool.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "DB에 연결할 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// user는 인증된 사용자와 관리자 권한을 확인한 뒤 핸들러를 호출한다.
func (a *API) user(next userHandler, admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. API 토큰을 사용자로 확인한다.
		user, err := a.Store.AuthenticateUser(r.Context(), bearer(r))
		if err != nil {
			a.authError(w, err)
			return
		}

		// 2. 관리자 전용 기능은 별도로 권한을 확인한다.
		if admin && !user.IsAdmin {
			writeError(w, http.StatusForbidden, "관리자 권한이 필요합니다")
			return
		}
		next(w, r, user)
	}
}

// authError는 인증 실패와 DB 장애를 구분하되 내부 정보를 감춘다.
func (a *API) authError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "유효한 인증 토큰이 필요합니다")
		return
	}
	a.fail(w, err)
}

// me는 토큰 소유자의 계정 정보만 반환한다.
func (a *API) me(w http.ResponseWriter, _ *http.Request, user store.User) {
	writeJSON(w, http.StatusOK, user)
}

// getPreferences는 본인 수신 항목 설정을 반환한다.
func (a *API) getPreferences(w http.ResponseWriter, r *http.Request, user store.User) {
	prefs, err := a.Store.GetPreferences(r.Context(), user.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// putPreferences는 기본값과 중복 없는 세부 수신 항목을 전체 교체한다.
func (a *API) putPreferences(w http.ResponseWriter, r *http.Request, user store.User) {
	// 1. 수신처와 분리된 개인 이벤트 선택을 검증한다.
	var prefs store.Preferences
	if !readJSON(w, r, &prefs) {
		return
	}
	seen := make(map[string]bool)
	valid := regexp.MustCompile(`^[a-z][a-z0-9_-]{0,127}\.[a-z][a-z0-9_-]{0,127}$`)
	if len(prefs.Items) > 1000 {
		writeError(w, http.StatusBadRequest, "수신 항목은 1000개까지 설정할 수 있습니다")
		return
	}
	for _, item := range prefs.Items {
		if !valid.MatchString(item.EventKey) || seen[item.EventKey] {
			writeError(w, http.StatusBadRequest, "수신 항목의 이름이나 중복 여부를 확인하세요")
			return
		}
		seen[item.EventKey] = true
	}

	// 2. 검증한 설정을 본인 계정에만 저장한다.
	if err := a.Store.SetPreferences(r.Context(), user.ID, prefs); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listDestinations는 자격 증명 없이 본인 수신처를 조회한다.
func (a *API) listDestinations(w http.ResponseWriter, r *http.Request, user store.User) {
	items, err := a.Store.ListDestinations(r.Context(), user.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// saveDestination은 채널 설정을 검증하고 암호화한 뒤 본인 수신처에 저장한다.
func (a *API) saveDestination(w http.ResponseWriter, r *http.Request, user store.User) {
	// 1. 채널별 설정과 표시 이름을 확인한다.
	var input struct {
		Name    string        `json:"name"`
		Kind    string        `json:"kind"`
		Enabled bool          `json:"enabled"`
		Config  notify.Config `json:"config"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !validName(input.Name) {
		writeError(w, http.StatusBadRequest, "수신처 이름은 공백을 제외한 1~100자로 입력하세요")
		return
	}
	if err := a.Sender.Validate(input.Kind, input.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 2. 자격 증명을 암호화하고 소유자 조건으로 저장한다.
	create := r.Method == http.MethodPost
	id := r.PathValue("id")
	if create {
		id = credential.NewToken("dest_")
	}
	config, _ := json.Marshal(input.Config)
	d := store.Destination{ID: id, Name: input.Name, Kind: input.Kind, Enabled: input.Enabled, EncryptedConfig: a.Cipher.Seal(config, id)}
	if err := a.Store.SaveDestination(r.Context(), user.ID, d, create); err != nil {
		a.fail(w, err)
		return
	}
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	writeJSON(w, status, d)
}

// deleteDestination은 본인 소유의 수신처만 삭제한다.
func (a *API) deleteDestination(w http.ResponseWriter, r *http.Request, user store.User) {
	if err := a.Store.DeleteDestination(r.Context(), user.ID, r.PathValue("id")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testDestination은 소유자의 요청으로 해당 수신처에 연결 확인 메시지를 발송한다.
func (a *API) testDestination(w http.ResponseWriter, r *http.Request, user store.User) {
	// 1. 본인의 수신처만 복호화한다.
	d, err := a.Store.GetDestination(r.Context(), user.ID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	data, err := a.Cipher.Open(d.EncryptedConfig, d.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	var config notify.Config
	if err = json.Unmarshal(data, &config); err != nil {
		a.fail(w, err)
		return
	}

	// 2. 이벤트 수신 여부와 무관하게 명시적으로 요청한 테스트를 보낸다.
	if err := a.Sender.Send(r.Context(), d.Kind, config, notify.Message{Title: "Whalechime 연결 확인", Body: "이 수신처로 알림을 받을 수 있습니다."}); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deliveries는 본인의 최근 발송 결과를 조회한다.
func (a *API) deliveries(w http.ResponseWriter, r *http.Request, user store.User) {
	items, err := a.Store.ListDeliveries(r.Context(), user.ID, queryLimit(r))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// eventTypes는 실제 관찰된 확장 항목을 포함해 수신 항목을 조회한다.
func (a *API) eventTypes(w http.ResponseWriter, r *http.Request, user store.User) {
	keys, err := a.Store.EventKeys(r.Context(), user.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// destinationTypes는 현재 구현된 알림 채널을 반환한다.
func (a *API) destinationTypes(w http.ResponseWriter, _ *http.Request, _ store.User) {
	writeJSON(w, http.StatusOK, notify.Kinds)
}

// servers는 본인에게 연결된 감시 서버를 조회한다.
func (a *API) servers(w http.ResponseWriter, r *http.Request, user store.User) {
	items, err := a.Store.ListServers(r.Context(), user.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// events는 본인 관련 서버의 이벤트를 필터링해 조회한다.
func (a *API) events(w http.ResponseWriter, r *http.Request, user store.User) {
	items, err := a.Store.ListEvents(r.Context(), user.ID, r.URL.Query().Get("server_id"), r.URL.Query().Get("event_key"), queryLimit(r))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// createUser는 관리자가 개인 설정 계정을 발급하게 한다.
func (a *API) createUser(w http.ResponseWriter, r *http.Request, _ store.User) {
	var input struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !validName(input.Name) {
		writeError(w, http.StatusBadRequest, "사용자 이름은 공백을 제외한 1~100자로 입력하세요")
		return
	}
	user, token, err := a.Store.CreateUser(r.Context(), input.Name)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user, "token": token})
}

// createServer는 관리자에게 서버 전용 에이전트 토큰을 발급한다.
func (a *API) createServer(w http.ResponseWriter, r *http.Request, user store.User) {
	var input struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !validName(input.Name) {
		writeError(w, http.StatusBadRequest, "서버 이름은 공백을 제외한 1~100자로 입력하세요")
		return
	}
	server, token, err := a.Store.CreateServer(r.Context(), input.Name, user.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"server": server, "token": token})
}

// setMember는 관리자가 서버와 관련 사용자의 연결을 설정하게 한다.
func (a *API) setMember(w http.ResponseWriter, r *http.Request, _ store.User) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled 값이 필요합니다")
		return
	}
	if err := a.Store.SetMember(r.Context(), r.PathValue("server"), r.PathValue("user"), *input.Enabled); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ingest는 서버 토큰으로 식별한 서버의 이벤트를 기록한다.
func (a *API) ingest(w http.ResponseWriter, r *http.Request) {
	// 1. 에이전트 토큰과 공통 이벤트 계약을 검증한다.
	serverID, err := a.Store.AuthenticateAgent(r.Context(), bearer(r))
	if err != nil {
		a.authError(w, err)
		return
	}
	var e event.Event
	if !readJSON(w, r, &e) {
		return
	}
	if err := e.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 2. 발송 대기와 함께 저장하고 DB 확정 후에만 수신 확인을 보낸다.
	id, created, err := a.Store.Ingest(r.Context(), serverID, e)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "created": created})
}

// heartbeat는 인증된 서버의 마지막 연결 시각을 갱신한다.
func (a *API) heartbeat(w http.ResponseWriter, r *http.Request) {
	serverID, err := a.Store.AuthenticateAgent(r.Context(), bearer(r))
	if err != nil {
		a.authError(w, err)
		return
	}
	if err := a.Store.Heartbeat(r.Context(), serverID); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fail은 저장소 오류를 비밀값 없는 HTTP 오류로 변환한다.
func (a *API) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "대상을 찾을 수 없습니다")
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "이미 등록된 이름입니다")
			return
		}
		if pgErr.Code == "23503" {
			writeError(w, http.StatusNotFound, "관련 대상을 찾을 수 없습니다")
			return
		}
	}
	a.Logger.Error("API 저장소 처리 실패")
	writeError(w, http.StatusInternalServerError, "요청을 처리할 수 없습니다")
}

// bearer는 Authorization 헤더에서 토큰만 추출한다.
func bearer(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

// validName은 공백 이름과 과도한 표시 이름을 거부한다.
func validName(name string) bool {
	return strings.TrimSpace(name) != "" && len([]rune(name)) <= 100 && !strings.ContainsAny(name, "\r\n")
}

// queryLimit은 목록 조회 개수를 기본 100개와 최대 500개로 제한한다.
func queryLimit(r *http.Request) int {
	value, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || value < 1 {
		return 100
	}
	return min(value, 500)
}

// readJSON은 본문 크기·알 수 없는 필드·추가 JSON 값을 검사한다.
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	// 1. 최대 본문과 명시된 입력 계약을 확인한다.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 본문과 필드, 크기를 확인하세요")
		return false
	}

	// 2. 첫 객체 뒤에 다른 값을 덧붙인 요청을 거부한다.
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "JSON 객체는 하나만 전달하세요")
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

// writeError는 일관된 오류 응답을 작성한다.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
