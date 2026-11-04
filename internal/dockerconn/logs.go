// Package dockerconn은 Docker 서버의 읽기 전용 연결을 제공한다.
package dockerconn

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
	"io"
	"regexp"
	"sync"
	"time"
)

var validContainerID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,255}$`)

// Container는 트레이에 표시하는 최소 컨테이너 정보다.
type Container struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// Frame은 로컬 로그 화면에 전달하는 출력 조각과 상태다.
type Frame struct {
	Kind   string `json:"kind"`
	Data   []byte `json:"data,omitempty"`
	Stream string `json:"stream,omitempty"`
	Error  string `json:"error,omitempty"`
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

// StreamLogs는 TTY 여부에 맞게 Docker 출력을 읽고 빈 로그도 연결 상태를 표시한다.
func StreamLogs(ctx context.Context, docker *client.Client, containerID string, encoder *json.Encoder) {
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
