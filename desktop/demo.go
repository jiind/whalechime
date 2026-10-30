package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// startDemo는 실제 Docker 출력으로 오해하지 않도록 표시한 트레이 미리보기를 제공한다.
func startDemo() (string, func()) {
	// 1. 두 서버와 서로 다른 컨테이너를 조회할 수 있는 독립 API를 준비한다.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html lang=ko><meta charset=utf-8><title>Whalechime 웹 미리보기</title><h1>웹으로 이동</h1><p>이 화면은 트레이 사용자 흐름 확인용 미리보기입니다. 컨테이너 로그 화면과 별도로 열립니다.</p></html>")
	})
	mux.HandleFunc("GET /api/servers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]server{{ID: "preview-prod", Name: "운영 서버 · 미리보기"}, {ID: "preview-stage", Name: "개발 서버 · 미리보기"}})
	})
	mux.HandleFunc("GET /api/servers/{server}/containers", func(w http.ResponseWriter, r *http.Request) {
		name := "payment-api"
		if r.PathValue("server") == "preview-stage" {
			name = "stage-api"
		}
		_ = json.NewEncoder(w).Encode([]container{{ID: name, Name: name, State: "running"}, {ID: name + "-worker", Name: name + "-worker", State: "running"}})
	})
	mux.HandleFunc("GET /api/servers/{server}/containers/{container}/logs", demoLogs)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(errPreview)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	return "http://" + listener.Addr().String(), func() {
		_ = server.Close()
	}
}

// demoLogs는 서버·컨테이너 선택과 자동 출력을 확인할 합성 로그를 전송한다.
func demoLogs(w http.ResponseWriter, r *http.Request) {
	// 1. 실제 서비스 로그가 아닌 미리보기임을 매 줄 명시한다.
	w.Header().Set("Content-Type", "application/x-ndjson")
	encoder := json.NewEncoder(w)
	_ = encoder.Encode(map[string]string{"kind": "ready"})
	controller := http.NewResponseController(w)
	_ = controller.Flush()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// 2. 사용자가 추가 버튼을 누르지 않아도 새 줄이 자동 표시되게 한다.
	for sequence := 1; ; sequence++ {
		line := fmt.Sprintf("%s [미리보기] %s INFO request completed sequence=%d status=200\n", time.Now().UTC().Format(time.RFC3339Nano), strings.ReplaceAll(r.PathValue("container"), "\n", " "), sequence)
		if err := encoder.Encode(map[string]any{"kind": "log", "stream": "stdout", "data": []byte(line)}); err != nil {
			return
		}
		if err := controller.Flush(); err != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
