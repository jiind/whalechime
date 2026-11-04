package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// connection은 사용자 토큰을 브라우저에 전달하지 않는 로컬 앱 연결이다.
type connection struct {
	webURL string
	base   string
	token  string
	http   *http.Client
}

// container는 상태 제어 없이 로그 대상으로 표시할 컨테이너다.
type container struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// server는 서버별 하위 메뉴에 표시할 조회 결과다.
type server struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Containers  []container `json:"containers,omitempty"`
	Unavailable bool        `json:"unavailable,omitempty"`
}

// newConnection은 URL과 토큰을 검증하고 전송 제한을 설정한다.
func newConnection(base, token string) (*connection, error) {
	// 1. 토큰을 URL에 넣거나 다른 주소로 재전송하지 않도록 검사한다.
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("로컬 앱 주소를 확인하세요")
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("로컬 앱 세션이 필요합니다")
	}

	// 2. 로그 스트림에는 전체 제한 시간 대신 연결·헤더 제한을 적용한다.
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, ResponseHeaderTimeout: 15 * time.Second, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 60 * time.Second}
	return &connection{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}}, nil
}

// get는 인증 헤더를 추가하고 상태 오류에 비밀값을 포함하지 않는다.
func (c *connection) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, errors.New("조회 요청을 만들 수 없습니다")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("로컬 앱에 연결할 수 없습니다")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("로컬 앱 HTTP %d", response.StatusCode)
	}
	return response, nil
}

// inventory는 관련 서버와 각 서버의 실제 컨테이너 목록을 병렬 조회한다.
func (c *connection) inventory(ctx context.Context) ([]server, error) {
	// 1. 로컬 앱에서 사용자가 접근할 수 있는 서버만 가져온다.
	response, err := c.get(ctx, "/api/servers")
	if err != nil {
		return nil, err
	}
	var servers []server
	err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&servers)
	response.Body.Close()
	if err != nil {
		return nil, errors.New("서버 목록을 읽을 수 없습니다")
	}
	if len(servers) > 100 {
		return nil, errors.New("트레이는 한 계정의 서버 100대까지 표시합니다")
	}

	// 2. 실패한 서버는 컨테이너가 없는 것처럼 표시하지 않는다.
	var work sync.WaitGroup
	limit := make(chan struct{}, 8)
	for i := range servers {
		work.Go(func() {
			limit <- struct{}{}
			defer func() {
				<-limit
			}()
			queryCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			response, err := c.get(queryCtx, "/api/servers/"+url.PathEscape(servers[i].ID)+"/containers")
			if err != nil {
				servers[i].Unavailable = true
				return
			}
			defer response.Body.Close()
			if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&servers[i].Containers); err != nil {
				servers[i].Unavailable = true
			}
			sort.Slice(servers[i].Containers, func(a, b int) bool {
				return servers[i].Containers[a].Name < servers[i].Containers[b].Name
			})
		})
	}
	work.Wait()
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Name < servers[j].Name
	})
	return servers, nil
}

// pageURL은 로그인 입력 없이 트레이에서 발급한 로컬 화면을 연다.
func (c *connection) pageURL() string {
	if c.webURL != "" {
		return c.webURL
	}
	return c.base + "/"
}
