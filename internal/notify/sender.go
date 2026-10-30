package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Kinds는 개인 수신처로 등록할 수 있는 채널 종류다.
var Kinds = []string{"email", "slack", "teams", "discord", "telegram", "google-chat", "mattermost", "ntfy", "gotify", "pushover", "webhook"}

// Config는 채널별 연결 정보를 담으며 저장 시 전체를 암호화한다.
type Config struct {
	URL      string `json:"url,omitempty"`
	Token    string `json:"token,omitempty"`
	ChatID   string `json:"chat_id,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	UserKey  string `json:"user_key,omitempty"`
}

// Message는 모든 활성 수신처에 동일하게 전달할 알림 내용이다.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"message"`
}

// Sender는 외부 연결 정책과 HTTP 연결 풀을 공유한다.
type Sender struct {
	client  *http.Client
	allowed map[string]bool
	dialer  net.Dialer
}

// NewSender는 내부 주소 예외 목록과 제한 시간이 있는 발송기를 만든다.
func NewSender(allowedHosts string) *Sender {
	// 1. 운영자가 명시한 내부 연동 대상만 예외로 허용한다.
	s := &Sender{allowed: make(map[string]bool), dialer: net.Dialer{Timeout: 10 * time.Second}}
	for _, host := range strings.Split(allowedHosts, ",") {
		s.allowed[strings.ToLower(strings.TrimSpace(host))] = true
	}

	// 2. 프록시와 리다이렉트를 사용하지 않고 실제 연결 주소도 검사한다.
	transport := &http.Transport{DialContext: s.dial, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxIdleConns: 32, IdleConnTimeout: 60 * time.Second}
	s.client = &http.Client{Transport: transport, Timeout: 25 * time.Second, CheckRedirect: stopRedirect}
	return s
}

// stopRedirect는 자격 증명을 다른 주소에 재전송하지 않도록 이동을 차단한다.
func stopRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

// dial은 DNS 조회 결과를 확인한 실제 IP로 연결해 내부 주소 우회를 막는다.
func (s *Sender) dial(ctx context.Context, network, address string) (net.Conn, error) {
	// 1. 운영자가 허용한 호스트는 사설 주소 연결도 허용한다.
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("수신처 주소가 올바르지 않습니다")
	}
	if s.allowed[strings.ToLower(host)] {
		return s.dialer.DialContext(ctx, network, address)
	}

	// 2. DNS 조회 결과 중 공개 주소로만 직접 연결한다.
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errors.New("수신처 주소 조회에 실패했습니다")
	}
	for _, candidate := range addresses {
		if !publicIP(candidate.IP) {
			continue
		}
		conn, err := s.dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("수신처 연결이 실패했거나 허용되지 않는 주소입니다")
}

// publicIP는 로컬·사설·링크 로컬·멀티캐스트와 특수 목적 대역을 제외한다.
func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48"} {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet.Contains(ip) {
			return false
		}
	}
	return true
}

// Validate는 채널별 필수 설정과 전송 가능한 URL 형식을 확인한다.
func (s *Sender) Validate(kind string, c Config) error {
	// 1. 헤더와 SMTP 명령에 줄바꿈이 섞이지 않도록 검사한다.
	for _, value := range []string{c.URL, c.Token, c.ChatID, c.Host, c.Username, c.Password, c.From, c.To, c.UserKey} {
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("연결 설정에 줄바꿈을 넣을 수 없습니다")
		}
	}

	// 2. 서비스마다 필수인 필드와 URL을 확인한다.
	switch kind {
	case "email":
		if c.Host == "" || c.Port < 1 || c.Port > 65535 || strings.ContainsAny(c.Host, "/:@ ") {
			return errors.New("SMTP host와 port를 확인하세요")
		}
		if _, err := mail.ParseAddress(c.From); err != nil {
			return errors.New("발신 이메일 주소를 확인하세요")
		}
		if _, err := mail.ParseAddress(c.To); err != nil {
			return errors.New("수신 이메일 주소를 확인하세요")
		}
		return nil
	case "telegram":
		if c.Token == "" || c.ChatID == "" || strings.ContainsAny(c.Token, "/?# ") {
			return errors.New("Telegram token과 chat_id를 확인하세요")
		}
		return nil
	case "pushover":
		if c.Token == "" || c.UserKey == "" {
			return errors.New("Pushover token과 user_key가 필요합니다")
		}
		return nil
	case "gotify":
		if c.Token == "" {
			return errors.New("Gotify token이 필요합니다")
		}
	case "slack", "teams", "discord", "google-chat", "mattermost", "ntfy", "webhook":
	default:
		return errors.New("지원하지 않는 수신 경로입니다")
	}
	parsed, err := url.Parse(c.URL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("수신처 URL을 확인하세요")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && s.allowed[strings.ToLower(parsed.Hostname())]) {
		return errors.New("HTTPS URL이 필요합니다. 내부 HTTP는 관리자의 호스트 허용 설정이 필요합니다")
	}
	return nil
}

// Send는 공통 메시지를 각 서비스의 요청 형식으로 변환해 발송한다.
func (s *Sender) Send(ctx context.Context, kind string, c Config, m Message) error {
	// 1. 저장된 설정도 발송 시 다시 검증한다.
	if err := s.Validate(kind, c); err != nil {
		return err
	}
	if kind == "email" {
		return s.sendMail(ctx, c, m)
	}

	// 2. 수신 서비스 형식에 맞춰 본문과 인증 정보를 구성한다.
	endpoint := c.URL
	text := m.Title + "\n" + m.Body
	var payload any
	headers := make(map[string]string)
	switch kind {
	case "slack", "google-chat", "mattermost":
		payload = map[string]string{"text": text}
	case "discord":
		payload = map[string]any{"content": truncate(text, 1900), "allowed_mentions": map[string]any{"parse": []string{}}}
	case "teams":
		payload = map[string]any{"type": "message", "attachments": []any{map[string]any{"contentType": "application/vnd.microsoft.card.adaptive", "content": map[string]any{"type": "AdaptiveCard", "version": "1.2", "body": []any{map[string]any{"type": "TextBlock", "text": m.Title, "weight": "Bolder", "wrap": true}, map[string]any{"type": "TextBlock", "text": m.Body, "wrap": true}}}}}}
	case "telegram":
		endpoint = "https://api.telegram.org/bot" + c.Token + "/sendMessage"
		payload = map[string]any{"chat_id": c.ChatID, "text": truncate(text, 4000)}
	case "gotify":
		endpoint = strings.TrimRight(c.URL, "/") + "/message"
		headers["X-Gotify-Key"] = c.Token
		payload = map[string]any{"title": m.Title, "message": m.Body, "priority": 5}
	case "pushover":
		endpoint = "https://api.pushover.net/1/messages.json"
		payload = map[string]string{"token": c.Token, "user": c.UserKey, "title": truncate(m.Title, 200), "message": truncate(m.Body, 1000)}
	case "ntfy":
		if c.Token != "" {
			headers["Authorization"] = "Bearer " + c.Token
		}
		return s.post(ctx, endpoint, "text/plain; charset=utf-8", []byte(text), headers)
	case "webhook":
		payload = m
		if c.Token != "" {
			headers["Authorization"] = "Bearer " + c.Token
		}
	}

	// 3. URL이나 토큰을 노출하지 않는 오류로 발송 결과를 반환한다.
	data, err := json.Marshal(payload)
	if err != nil {
		return errors.New("알림 본문 생성에 실패했습니다")
	}
	return s.post(ctx, endpoint, "application/json", data, headers)
}

// post는 응답 크기를 제한하고 비밀값 없이 HTTP 상태만 반환한다.
func (s *Sender) post(ctx context.Context, endpoint, contentType string, data []byte, headers map[string]string) error {
	// 1. 문맥 취소와 요청 제한 시간을 적용한다.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("알림 요청을 만들 수 없습니다")
	}
	req.Header.Set("Content-Type", contentType)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return errors.New("알림 전송 연결에 실패했습니다")
	}
	defer response.Body.Close()

	// 2. 서비스 응답 내용에 포함될 수 있는 자격 증명을 저장하지 않는다.
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("알림 서비스 HTTP %d", response.StatusCode)
	}
	return nil
}

// sendMail은 TLS 연결 또는 STARTTLS를 사용해 메일을 발송한다.
func (s *Sender) sendMail(ctx context.Context, c Config, m Message) error {
	// 1. 수신처 주소 정책을 적용한 연결에서 TLS를 설정한다.
	conn, err := s.dial(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return errors.New("SMTP 연결에 실패했습니다")
	}
	defer conn.Close()
	deadline := time.Now().Add(25 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = conn.SetDeadline(deadline)
	tlsConfig := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.Port == 465 {
		conn = tls.Client(conn, tlsConfig)
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return errors.New("SMTP 초기화에 실패했습니다")
	}
	defer client.Close()
	if c.Port != 465 {
		if err := client.StartTLS(tlsConfig); err != nil {
			return errors.New("SMTP STARTTLS에 실패했습니다")
		}
	}

	// 2. 인증과 메일 봉투를 구성한다.
	if c.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return errors.New("SMTP 인증에 실패했습니다")
		}
	}
	from, _ := mail.ParseAddress(c.From)
	to, _ := mail.ParseAddress(c.To)
	if err := client.Mail(from.Address); err != nil {
		return errors.New("SMTP 발신 주소를 사용할 수 없습니다")
	}
	if err := client.Rcpt(to.Address); err != nil {
		return errors.New("SMTP 수신 주소를 사용할 수 없습니다")
	}

	// 3. 고정 제목과 UTF-8 본문을 전송하고 서버의 수신 확인을 기다린다.
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP 본문 전송을 시작할 수 없습니다")
	}
	body := "From: " + from.String() + "\r\nTo: " + to.String() + "\r\nSubject: Whalechime notification\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + m.Title + "\r\n" + m.Body
	if _, err := io.WriteString(writer, body); err != nil {
		return errors.New("SMTP 본문 전송에 실패했습니다")
	}
	if err := writer.Close(); err != nil {
		return errors.New("SMTP 메일 접수에 실패했습니다")
	}
	return nil
}

// truncate는 멀티바이트 문자를 자르지 않고 채널의 본문 길이를 제한한다.
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}
