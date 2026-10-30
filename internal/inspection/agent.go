package inspection

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// RunAgent는 제한된 병렬도로 중앙 서버의 읽기 전용 요청을 수행한다.
func RunAgent(ctx context.Context, docker *client.Client, serverURL, token string) {
	// 1. 수집 연결과 독립된 읽기 요청 연결 풀을 준비한다.
	httpClient := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	defer httpClient.CloseIdleConnections()
	var running sync.WaitGroup
	defer running.Wait()
	semaphore := make(chan struct{}, 8)
	base := strings.TrimRight(serverURL, "/") + "/api/agent/inspection/tasks"

	// 2. 동시에 처리 가능한 만큼만 요청을 받고 종료 시 모두 정리한다.
	for ctx.Err() == nil {
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			return
		}
		pollCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		task, err := pollTask(pollCtx, httpClient, base, token)
		cancel()
		if err != nil || task.ID == "" {
			<-semaphore
			if err != nil && !pause(ctx, time.Second) {
				return
			}
			continue
		}
		running.Go(func() {
			defer func() {
				<-semaphore
			}()
			executeTask(ctx, docker, httpClient, base, token, task)
		})
	}
}

// pollTask는 에이전트 토큰을 헤더에 넣어 읽기 요청 하나를 가져온다.
func pollTask(ctx context.Context, c *http.Client, endpoint, token string) (Task, error) {
	// 1. 리다이렉트 없이 중앙 서버에 인증한다.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Task{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := c.Do(req)
	if err != nil {
		return Task{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return Task{}, nil
	}
	if response.StatusCode != http.StatusOK {
		return Task{}, errors.New("읽기 요청 조회 실패")
	}

	// 2. 서버가 보낸 작업 크기를 제한해 읽는다.
	var task Task
	err = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&task)
	return task, err
}

// frameWriter는 Docker 출력 조각을 크기가 제한된 전송 프레임으로 바꾼다.
type frameWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	stream  string
}

// Write는 긴 로그도 제한된 조각으로 나눠 역압력을 유지하며 전송한다.
func (f *frameWriter) Write(data []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	written := 0
	for len(data) > 0 {
		n := min(len(data), 16<<10)
		if err := f.encoder.Encode(Frame{Kind: "log", Stream: f.stream, Data: data[:n]}); err != nil {
			return written, err
		}
		written += n
		data = data[n:]
	}
	return written, nil
}

// executeTask는 목록 조회 또는 로그 읽기만 수행해 응답 스트림으로 전송한다.
func executeTask(parent context.Context, docker *client.Client, c *http.Client, base, token string, task Task) {
	// 1. 응답 업로드가 닫히면 Docker 조회도 취소되는 연결을 만든다.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/"+task.ID+"/frames", reader)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-ndjson")
	uploadDone := make(chan struct{})
	go func() {
		defer close(uploadDone)
		defer cancel()
		response, err := c.Do(req)
		if err == nil {
			response.Body.Close()
		}
		reader.Close()
	}()
	encoder := json.NewEncoder(writer)

	// 2. 사용자 지정 명령 없이 허용된 조회만 호출한다.
	switch task.Operation {
	case "containers":
		listCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		result, err := docker.ContainerList(listCtx, client.ContainerListOptions{All: true})
		stop()
		if err != nil {
			_ = encoder.Encode(Frame{Kind: "error", Error: "Docker 컨테이너 목록을 조회할 수 없습니다"})
			break
		}
		items := make([]Container, 0, len(result.Items))
		for _, item := range result.Items {
			name := item.ID[:min(12, len(item.ID))]
			if len(item.Names) > 0 {
				name = strings.TrimPrefix(item.Names[0], "/")
			}
			items = append(items, Container{ID: item.ID, Name: name, State: string(item.State)})
		}
		_ = encoder.Encode(Frame{Kind: "containers", Containers: items})
	case "logs":
		streamLogs(ctx, docker, task.ContainerID, encoder)
	default:
		_ = encoder.Encode(Frame{Kind: "error", Error: "허용되지 않은 조회 작업입니다"})
	}

	// 3. 본문 전송을 끝내고 HTTP 연결이 정리될 때까지 기다린다.
	writer.Close()
	<-uploadDone
}

// streamLogs는 TTY 여부에 맞게 Docker 출력을 읽고 빈 로그도 연결 상태를 표시한다.
func streamLogs(ctx context.Context, docker *client.Client, containerID string, encoder *json.Encoder) {
	// 1. 컨테이너 설정 조회로 TTY 형식을 확인한다.
	if !validContainerID.MatchString(containerID) {
		_ = encoder.Encode(Frame{Kind: "error", Error: "컨테이너 식별자가 올바르지 않습니다"})
		return
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	result, err := docker.ContainerInspect(inspectCtx, containerID, client.ContainerInspectOptions{})
	cancel()
	if err != nil || result.Container.Config == nil {
		_ = encoder.Encode(Frame{Kind: "error", Error: "컨테이너를 조회할 수 없습니다"})
		return
	}
	logs, err := docker.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true, Follow: true, Tail: "200"})
	if err != nil {
		_ = encoder.Encode(Frame{Kind: "error", Error: "Docker에서 이 컨테이너의 로그를 읽을 수 없습니다"})
		return
	}
	defer logs.Close()

	// 2. 유휴 상태에서도 연결 유지 프레임을 보내며 출력을 직렬화한다.
	output := &frameWriter{encoder: encoder, stream: "stdout"}
	_ = encoder.Encode(Frame{Kind: "ready"})
	heartbeatCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for pause(heartbeatCtx, 10*time.Second) {
			output.mu.Lock()
			err := encoder.Encode(Frame{Kind: "heartbeat"})
			output.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	if result.Container.Config.Tty {
		_, err = io.Copy(output, bufio.NewReader(logs))
	} else {
		// Docker의 두 스트림은 순차로 전달되며 한 인코더를 사용한다.
		_, err = stdcopy.StdCopy(output, &stderrWriter{output: output}, logs)
	}

	// 3. 종료·연결 끊김을 명시적으로 표시한다.
	stop()
	<-done
	if err != nil && ctx.Err() == nil {
		_ = encoder.Encode(Frame{Kind: "error", Error: "Docker 로그 연결이 끊겼습니다"})
		return
	}
	_ = encoder.Encode(Frame{Kind: "done"})
}

// stderrWriter는 같은 인코더 잠금 안에서 stderr 프레임을 생성한다.
type stderrWriter struct {
	output *frameWriter
}

// Write는 stderr 표시를 유지하며 stdout과 동일한 전송 제한을 적용한다.
func (s *stderrWriter) Write(data []byte) (int, error) {
	// 1. 두 출력과 연결 유지 프레임이 같은 잠금을 공유하게 한다.
	s.output.mu.Lock()
	defer s.output.mu.Unlock()
	written := 0

	// 2. stderr 출력을 크기가 제한된 프레임으로 전송한다.
	for len(data) > 0 {
		n := min(len(data), 16<<10)
		if err := s.output.encoder.Encode(Frame{Kind: "log", Stream: "stderr", Data: data[:n]}); err != nil {
			return written, err
		}
		written += n
		data = data[n:]
	}
	return written, nil
}

// pause는 읽기 요청 재시도와 유휴 대기를 종료 신호에 맞춰 중단한다.
func pause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
