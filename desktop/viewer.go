package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

//go:embed assets/*
var assets embed.FS

// target은 사용자가 트레이에서 선택한 로그 대상이다.
type target struct {
	ServerID      string
	ServerName    string
	ContainerID   string
	ContainerName string
}

// viewer는 로컬 앱 세션을 숨긴 로컬 로그 전용 화면을 제공한다.
type viewer struct {
	connection *connection
	base       string
	mu         sync.Mutex
	views      map[string]target
	order      []string
	page       *template.Template
}

// newViewer는 루프백 임의 포트에만 바인딩하고 선택 대상별 비밀 경로를 발급한다.
func newViewer(c *connection) (*viewer, *http.Server, net.Listener, error) {
	// 1. 로컬 화면과 로그 전달 경로를 준비한다.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, nil, err
	}
	page := template.Must(template.ParseFS(assets, "assets/viewer.html"))
	v := &viewer{connection: c, base: "http://" + listener.Addr().String(), views: make(map[string]target), page: page}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /view/{view}", v.show)
	mux.HandleFunc("GET /view/{view}/stream", v.stream)
	mux.HandleFunc("GET /viewer.js", v.asset)
	mux.HandleFunc("GET /viewer.css", v.asset)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// 2. 외부 사이트의 프레임·호스트 변경을 차단한다.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != listener.Addr().String() {
			http.Error(w, "허용되지 않는 호스트입니다", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	return v, server, listener, nil
}

// open은 메뉴에서 이미 선택한 대상만 조회할 수 있는 로그 화면 주소를 발급한다.
func (v *viewer) open(t target) string {
	// 1. 사용자 토큰 대신 일회성 실행 범위의 무작위 화면 식별자를 사용한다.
	data := make([]byte, 32)
	_, _ = rand.Read(data)
	id := base64.RawURLEncoding.EncodeToString(data)
	v.mu.Lock()
	defer v.mu.Unlock()
	v.views[id] = t
	v.order = append(v.order, id)

	// 2. 창을 반복해 열어도 대상 정보가 무제한 쌓이지 않게 제한한다.
	if len(v.order) > 64 {
		delete(v.views, v.order[0])
		v.order = v.order[1:]
	}
	return v.base + "/view/" + id
}

// selected는 URL로 임의 서버를 지정하지 못하도록 등록된 선택만 조회한다.
func (v *viewer) selected(r *http.Request) (target, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	t, ok := v.views[r.PathValue("view")]
	return t, ok
}

// show는 별도 선택 폼이 없는 로그 전용 화면을 출력한다.
func (v *viewer) show(w http.ResponseWriter, r *http.Request) {
	t, ok := v.selected(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = v.page.Execute(w, t)
}

// stream은 트레이에서 선택한 컨테이너의 로그만 로컬 앱 세션으로 중계한다.
func (v *viewer) stream(w http.ResponseWriter, r *http.Request) {
	// 1. 화면 식별자로 고정된 대상에만 인증된 읽기 요청을 보낸다.
	t, ok := v.selected(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	path := "/api/servers/" + url.PathEscape(t.ServerID) + "/containers/" + url.PathEscape(t.ContainerID) + "/logs"
	response, err := v.connection.get(r.Context(), path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	controller := http.NewResponseController(w)
	_ = controller.Flush()

	// 2. 브라우저가 닫히면 Docker 조회 연결도 함께 취소한다.
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}

// asset는 외부 자산 없이 로그 화면의 스크립트와 스타일을 제공한다.
func (v *viewer) asset(w http.ResponseWriter, r *http.Request) {
	name := "assets" + r.URL.Path
	file, err := assets.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	if r.URL.Path == "/viewer.js" {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	_, _ = io.Copy(w, file)
}

// stopServer는 앱 종료 시 로컬 스트림도 즉시 닫는다.
func stopServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	_ = server.Close()
}
