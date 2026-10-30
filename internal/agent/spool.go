package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jiind/whalechime/internal/event"
	"golang.org/x/sys/unix"
)

// Spool은 미확인 이벤트와 Docker 재연결 커서를 디스크에 보관한다.
type Spool struct {
	dir    string
	lock   *os.File
	mu     sync.Mutex
	cursor int64
}

// OpenSpool은 디렉터리를 독점 잠금하고 마지막 수집 시각을 읽는다.
func OpenSpool(dir string) (*Spool, error) {
	// 1. 이벤트 파일을 보호할 디렉터리와 프로세스 잠금을 준비한다.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("이미 사용 중인 에이전트 데이터 디렉터리입니다")
	}
	s := &Spool{dir: dir, lock: lock, cursor: time.Now().Add(-time.Second).UnixNano()}

	// 2. 커서가 있으면 재사용하고 최초 시작 시각도 영속화한다.
	data, err := os.ReadFile(filepath.Join(dir, "cursor"))
	if err == nil {
		s.cursor, err = strconv.ParseInt(string(data), 10, 64)
	} else if errors.Is(err, os.ErrNotExist) {
		err = s.atomicWrite("cursor", []byte(strconv.FormatInt(s.cursor, 10)))
	}
	if err != nil || s.cursor <= 0 {
		s.Close()
		return nil, errors.New("에이전트 커서를 읽거나 저장할 수 없습니다")
	}
	return s, nil
}

// Close는 프로세스 잠금을 해제한다.
func (s *Spool) Close() error {
	return s.lock.Close()
}

// Since는 마지막 수집 시각을 포함해 재조회할 Docker 커서를 반환한다.
func (s *Spool) Since() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Unix(0, s.cursor).UTC().Format(time.RFC3339Nano)
}

// Append는 이벤트를 먼저 영속화한 뒤 재연결 커서를 전진시킨다.
func (s *Spool) Append(e event.Event) error {
	// 1. 업로드 확인 전에도 복구할 수 있도록 이벤트 파일을 확정한다.
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := s.atomicWrite(eventFilename(e), data); err != nil {
		return err
	}

	// 2. 커서보다 오래된 재전송은 커서를 뒤로 이동시키지 않는다.
	if e.TimeNano > s.cursor {
		if err := s.atomicWrite("cursor", []byte(strconv.FormatInt(e.TimeNano, 10))); err != nil {
			return err
		}
		s.cursor = e.TimeNano
	}
	return nil
}

// Pending은 시간 순서로 첫 미확인 이벤트를 읽는다.
func (s *Spool) Pending() (event.Event, error) {
	// 1. 원자적 파일 교체 중인 임시 파일과 커서는 제외한다.
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return event.Event{}, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// 2. 가장 오래된 이벤트가 손상되면 건너뛰지 않고 오류를 알린다.
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return event.Event{}, err
		}
		var e event.Event
		if err := json.Unmarshal(data, &e); err != nil {
			return e, err
		}
		return e, e.Validate()
	}
	return event.Event{}, os.ErrNotExist
}

// Acknowledge는 중앙 서버가 DB 저장을 확인한 이벤트만 제거한다.
func (s *Spool) Acknowledge(e event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(filepath.Join(s.dir, eventFilename(e))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.syncDirectory()
}

// eventFilename은 시각 정렬과 중복 덮어쓰기가 가능한 파일 이름을 만든다.
func eventFilename(e event.Event) string {
	return fmt.Sprintf("%020d-%s.json", e.TimeNano, e.ID(""))
}

// atomicWrite는 내용과 이름 변경을 디스크에 확정한다.
func (s *Spool) atomicWrite(name string, data []byte) error {
	// 1. 완료되지 않은 파일이 업로드 대상으로 보이지 않게 임시 파일에 쓴다.
	file, err := os.CreateTemp(s.dir, ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	// 2. 파일 이름과 디렉터리 변경도 확정한다.
	if err := os.Rename(file.Name(), filepath.Join(s.dir, name)); err != nil {
		return err
	}
	return s.syncDirectory()
}

// syncDirectory는 파일 추가·삭제의 디렉터리 정보를 디스크에 반영한다.
func (s *Spool) syncDirectory() error {
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
