package notify

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestChannelPayloads는 서비스별 HTTP 요청 형식과 일반 Webhook 인증을 확인한다.
func TestChannelPayloads(t *testing.T) {
	for _, kind := range []string{"slack", "teams", "discord", "google-chat", "mattermost", "ntfy", "gotify", "webhook"} {
		t.Run(kind, func(t *testing.T) {
			// 1. 실제 HTTP 수신 서버에서 채널별 본문을 확인한다.
			var called atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called.Store(true)
				if r.Method != http.MethodPost {
					t.Error("POST 요청이 아닙니다")
				}
				data, _ := io.ReadAll(r.Body)
				if kind == "ntfy" {
					if string(data) != "제목\n본문" || r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("ntfy 본문 또는 인증이 잘못되었습니다")
					}
				} else {
					var payload map[string]any
					if err := json.Unmarshal(data, &payload); err != nil {
						t.Error(err)
					}
					if kind == "teams" && payload["type"] != "message" {
						t.Error("Teams 메시지 봉투가 없습니다")
					}
					if kind == "discord" && payload["content"] != "제목\n본문" {
						t.Error("Discord 본문이 잘못되었습니다")
					}
					if kind == "webhook" && r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("Webhook 인증이 없습니다")
					}
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			// 2. 운영자가 허용한 로컬 수신처에 실제 전송한다.
			sender := NewSender("127.0.0.1")
			if err := sender.Send(context.Background(), kind, Config{URL: server.URL, Token: "secret"}, Message{Title: "제목", Body: "본문"}); err != nil {
				t.Fatal(err)
			}
			if !called.Load() {
				t.Fatal("수신 서버가 호출되지 않았습니다")
			}
		})
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

// RoundTrip은 외부 계정에 전송하지 않고 제공자 요청 계약만 검사한다.
func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// TestFixedProviderPayloads는 고정 API 주소를 사용하는 채널의 인증·본문 계약을 확인한다.
func TestFixedProviderPayloads(t *testing.T) {
	for _, kind := range []string{"telegram", "pushover"} {
		t.Run(kind, func(t *testing.T) {
			// 1. 네트워크 호출 경계를 가로채 외부 서비스로 전송하지 않는다.
			sender := NewSender("")
			sender.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if kind == "telegram" {
					if r.URL.Host != "api.telegram.org" || r.URL.Path != "/bot123:token/sendMessage" || payload["chat_id"] != "chat-1" || payload["text"] != "제목\n본문" {
						t.Fatal("Telegram API 계약이 올바르지 않습니다")
					}
				} else if r.URL.Host != "api.pushover.net" || payload["token"] != "123:token" || payload["user"] != "user-key" || payload["message"] != "본문" {
					t.Fatal("Pushover API 계약이 올바르지 않습니다")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
			})

			// 2. 실제 발송 경로가 만든 요청이 해당 서비스 계약을 만족해야 한다.
			if err := sender.Send(context.Background(), kind, Config{Token: "123:token", ChatID: "chat-1", UserKey: "user-key"}, Message{Title: "제목", Body: "본문"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestNetworkPolicy는 사설 주소 우회와 자격 증명 리다이렉트를 차단하는지 확인한다.
func TestNetworkPolicy(t *testing.T) {
	// 1. 기본 정책에서 내부 주소와 특수 목적 대역을 차단한다.
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "64:ff9b::7f00:1"} {
		if publicIP(net.ParseIP(address)) {
			t.Fatalf("내부 주소를 허용했습니다: %s", address)
		}
	}
	sender := NewSender("")
	if err := sender.Validate("webhook", Config{URL: "http://127.0.0.1/"}); err == nil {
		t.Fatal("허용되지 않은 HTTP 수신처를 등록했습니다")
	}

	// 2. 허용한 수신처도 다른 URL로 리다이렉트하면 성공으로 처리하지 않는다.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	if err := NewSender("127.0.0.1").Send(context.Background(), "webhook", Config{URL: server.URL}, Message{}); err == nil {
		t.Fatal("리다이렉트를 허용했습니다")
	}
}
