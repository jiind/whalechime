package agent

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jiind/whalechime/internal/event"
)

// TestSpoolRecovery는 재시작·재전송·시간 역전에서도 미확인 이벤트가 보존되는지 확인한다.
func TestSpoolRecovery(t *testing.T) {
	// 1. 같은 디렉터리를 두 프로세스가 공유하지 못하는지 확인한다.
	dir := t.TempDir()
	s, err := OpenSpool(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenSpool(dir); err == nil {
		other.Close()
		t.Fatal("중복 에이전트가 스풀을 열었습니다")
	}
	newer := event.Event{Type: "image", Action: "push", ActorID: "new", TimeNano: time.Now().Add(time.Second).UnixNano()}
	older := event.Event{Type: "image", Action: "pull", ActorID: "old", TimeNano: newer.TimeNano - 1}
	for _, e := range []event.Event{newer, older, newer} {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	cursor := s.Since()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. 재시작 후 오래된 미확인 이벤트부터 전송하고 커서는 뒤로 가지 않는다.
	s, err = OpenSpool(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Since() != cursor {
		t.Fatal("커서 복구 실패")
	}
	for _, expected := range []event.Event{older, newer} {
		actual, err := s.Pending()
		if err != nil || actual.ActorID != expected.ActorID {
			t.Fatalf("미확인 이벤트 복구 실패: %+v, %v", actual, err)
		}
		if err := s.Acknowledge(actual); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Pending(); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("확인한 이벤트가 스풀에 남았습니다")
	}
}

// TestConcurrentSpool은 수집과 조회가 동시에 수행되어도 파일이 손상되지 않는지 확인한다.
func TestConcurrentSpool(t *testing.T) {
	s, err := OpenSpool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var group sync.WaitGroup
	for i := range 10 {
		group.Go(func() {
			if err := s.Append(event.Event{Type: "image", Action: "pull", TimeNano: time.Now().UnixNano() + int64(i)}); err != nil {
				t.Error(err)
			}
			_, _ = s.Pending()
		})
	}
	group.Wait()
}
