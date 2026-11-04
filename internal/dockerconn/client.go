// Package dockerconn은 서버별 인증 설정으로 Docker의 읽기 전용 API에 연결한다.
package dockerconn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/moby/moby/client"
)

// Server는 원격 Docker 연결 정보이며 개인키 원문 대신 파일 경로만 보관한다.
type Server struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Address      string `json:"address"`
	Enabled      bool   `json:"enabled"`
	MTLS         bool   `json:"mtls"`
	CAFile       string `json:"ca_file"`
	CertFile     string `json:"cert_file"`
	KeyFile      string `json:"key_file"`
	IdentityFile string `json:"identity_file"`
}

var safeHost = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]*$`)
var readPath = regexp.MustCompile(`^(/v[0-9]+\.[0-9]+)?/(_ping|version|events|containers/json|containers/[a-zA-Z0-9_.-]+/(json|logs))$`)

// Validate는 연결 방식에 맞는 주소와 인증 설정의 조합을 검사한다.
func (s Server) Validate() error {
	// 1. 표시 이름과 API 루트 주소를 확인한다.
	if strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 100 || strings.ContainsAny(s.Name, "\r\n") {
		return errors.New("서버 이름은 1~100자로 입력하세요")
	}
	u, err := url.Parse(s.Address)
	if err != nil || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("서버 주소에는 호스트와 포트만 지정하세요")
	}
	if port := u.Port(); port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return errors.New("서버 포트를 확인하세요")
		}
	}

	// 2. HTTP에는 TLS 설정을 섞지 않고 SSH 비밀번호는 저장하지 않는다.
	switch u.Scheme {
	case "http", "https":
		if u.User != nil || s.IdentityFile != "" {
			return errors.New("API 주소의 사용자 정보와 SSH 키 설정은 사용할 수 없습니다")
		}
		if u.Scheme == "http" && (s.MTLS || s.CAFile != "" || s.CertFile != "" || s.KeyFile != "") {
			return errors.New("인증서와 mTLS는 HTTPS에서만 사용할 수 있습니다")
		}
		if s.MTLS && (s.CertFile == "" || s.KeyFile == "") {
			return errors.New("mTLS에는 클라이언트 인증서와 개인키 파일이 필요합니다")
		}
		if !s.MTLS && (s.CertFile != "" || s.KeyFile != "") {
			return errors.New("클라이언트 인증서를 사용하려면 mTLS를 켜세요")
		}
	case "ssh":
		if !safeHost.MatchString(u.Hostname()) || s.MTLS || s.CAFile != "" || s.CertFile != "" || s.KeyFile != "" {
			return errors.New("SSH 호스트와 연결 설정을 확인하세요")
		}
		if u.User != nil {
			if _, exists := u.User.Password(); exists || !safeHost.MatchString(u.User.Username()) {
				return errors.New("SSH에는 비밀번호 없이 사용자 이름만 지정하세요")
			}
		}
	default:
		return errors.New("http://, https:// 또는 ssh:// 주소를 사용하세요")
	}
	return nil
}

// New는 환경변수와 독립적으로 서버별 전송·인증·읽기 전용 정책을 구성한다.
func New(s Server) (*client.Client, error) {
	// 1. 서버 인증서를 항상 검증하고 선택한 경우에만 클라이언트 인증서를 추가한다.
	if err := s.Validate(); err != nil {
		return nil, err
	}
	u, _ := url.Parse(s.Address)
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 30 * time.Second}
	if u.Scheme == "https" {
		config := &tls.Config{MinVersion: tls.VersionTLS12}
		if s.CAFile != "" {
			pem, err := os.ReadFile(s.CAFile)
			if err != nil {
				return nil, errors.New("CA 인증서 파일을 읽을 수 없습니다")
			}
			config.RootCAs, _ = x509.SystemCertPool()
			if config.RootCAs == nil {
				config.RootCAs = x509.NewCertPool()
			}
			if !config.RootCAs.AppendCertsFromPEM(pem) {
				return nil, errors.New("CA 인증서 형식을 확인하세요")
			}
		}
		if s.MTLS {
			cert, err := tls.LoadX509KeyPair(s.CertFile, s.KeyFile)
			if err != nil {
				return nil, errors.New("클라이언트 인증서와 개인키 파일을 확인하세요")
			}
			config.Certificates = []tls.Certificate{cert}
		}
		transport.TLSClientConfig = config
	}

	// 2. SSH도 동일한 Docker API를 운반하고 임의 셸 명령은 받지 않는다.
	host := s.Address
	if u.Scheme == "ssh" {
		if _, err := exec.LookPath("ssh"); err != nil {
			return nil, errors.New("SSH 연결에는 OpenSSH 클라이언트가 필요합니다")
		}
		transport.DialContext = sshDialer(s)
		host = "http://docker"
	}
	httpClient := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	scheme, _ := url.Parse(host)
	return client.New(client.WithHost(strings.TrimRight(host, "/")), client.WithHTTPClient(httpClient), client.WithScheme(scheme.Scheme), client.WithHTTPRequestHook(allowRead), client.WithUserAgent("whalechime-local/0.1.0"))
}

// allowRead는 목록·이벤트·로그와 버전 협상에 필요한 조회만 허용한다.
func allowRead(r *http.Request) error {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || !readPath.MatchString(r.URL.Path) {
		return errors.New("허용되지 않은 Docker 요청입니다")
	}
	return nil
}

// sshArgs는 기존 SSH 설정과 엄격한 호스트 확인을 사용하는 고정 명령을 만든다.
func sshArgs(s Server) []string {
	// 1. 무인 연결에서 비밀번호와 호스트 신뢰 여부를 묻지 않는다.
	u, _ := url.Parse(s.Address)
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10"}
	if s.IdentityFile != "" {
		args = append(args, "-i", s.IdentityFile)
	}
	if u.Port() != "" {
		args = append(args, "-p", u.Port())
	}
	if u.User != nil {
		args = append(args, "-l", u.User.Username())
	}

	// 2. 원격에서는 Docker 소켓에 표준 입출력을 연결하는 명령만 실행한다.
	return append(args, "--", u.Hostname(), "docker", "system", "dial-stdio")
}

// sshDialer는 HTTP 연결마다 SSH 표준 입출력을 연결하고 종료 시 프로세스를 회수한다.
func sshDialer(s Server) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		// 1. 기존 SSH 키·에이전트·설정을 사용하는 프로세스를 준비한다.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		commandCtx, cancel := context.WithCancel(context.Background())
		command := exec.CommandContext(commandCtx, "ssh", sshArgs(s)...)
		input, err := command.StdinPipe()
		if err != nil {
			cancel()
			return nil, err
		}
		output, err := command.StdoutPipe()
		if err != nil {
			input.Close()
			cancel()
			return nil, err
		}
		if err := command.Start(); err != nil {
			input.Close()
			output.Close()
			cancel()
			return nil, errors.New("SSH 연결을 시작할 수 없습니다")
		}

		// 2. HTTP의 연결 취소·제한 시간을 양방향 파이프 종료에 연결한다.
		local, remote := net.Pipe()
		go func() {
			defer cancel()
			defer remote.Close()
			defer input.Close()
			_, _ = io.Copy(input, remote)
		}()
		go func() {
			_, _ = io.Copy(remote, output)
			remote.Close()
			cancel()
			_ = command.Wait()
		}()
		return local, nil
	}
}
