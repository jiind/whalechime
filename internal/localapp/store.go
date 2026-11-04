// Package localapp은 한 사용자의 Docker 연결·수신 설정·이력을 PC에 보관한다.
package localapp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/dockerconn"
	"github.com/jiind/whalechime/internal/event"
	"github.com/jiind/whalechime/internal/notify"
)

// Preferences는 이벤트 선택과 수신처를 독립적으로 관리한다.
type Preferences struct {
	DefaultEnabled bool         `json:"default_enabled"`
	Items          []Preference `json:"items"`
}

// Preference는 한 이벤트 항목의 수신 여부다.
type Preference struct {
	EventKey string `json:"event_key"`
	Enabled  bool   `json:"enabled"`
}

// Destination은 암호화할 연결 설정과 공개 메타데이터를 함께 보관한다.
type Destination struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Kind    string        `json:"kind"`
	Enabled bool          `json:"enabled"`
	Config  notify.Config `json:"config,omitempty"`
}

// Record는 수집 서버와 Docker 이벤트를 함께 보관한다.
type Record struct {
	ID         string      `json:"id"`
	ServerID   string      `json:"server_id"`
	ServerName string      `json:"server_name"`
	EventKey   string      `json:"event_key"`
	OccurredAt time.Time   `json:"occurred_at"`
	Payload    event.Event `json:"payload"`
}

// Delivery는 이벤트 이력 정리와 독립적으로 재시도할 발송 작업이다.
type Delivery struct {
	ID              string     `json:"id"`
	DestinationID   string     `json:"destination_id"`
	DestinationName string     `json:"destination_name"`
	EventID         string     `json:"event_id"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	SentAt          *time.Time `json:"sent_at"`
	AvailableAt     time.Time  `json:"available_at"`
	LastError       string     `json:"last_error"`
}

// job은 공개 발송 이력에 원본 이벤트를 포함하지 않는 디스크 저장 계약이다.
type job struct {
	Delivery
	Source Record `json:"source"`
}

// state는 한 번의 원자적 교체로 저장하는 로컬 상태다.
type state struct {
	Version      int                 `json:"version"`
	Servers      []dockerconn.Server `json:"servers"`
	Preferences  Preferences         `json:"preferences"`
	Destinations []Destination       `json:"destinations"`
	Events       []Record            `json:"events"`
	Jobs         []job               `json:"jobs"`
	Cursors      map[string]int64    `json:"cursors"`
}

// diskStore는 프로세스·고루틴 잠금과 암호화된 상태 파일을 소유한다.
type diskStore struct {
	mu     sync.Mutex
	dir    string
	lock   *os.File
	cipher *credential.Cipher
	data   state
}

// openStore는 외부 DB 없이 사용자 전용 파일과 암호화 키를 준비한다.
func openStore(dir string) (*diskStore, error) {
	// 1. 같은 데이터 디렉터리를 여러 앱에서 동시에 쓰지 못하게 한다.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, errors.New("앱 데이터 디렉터리를 만들 수 없습니다")
	}
	lock, err := lockDirectory(dir)
	if err != nil {
		return nil, err
	}
	s := &diskStore{dir: dir, lock: lock}
	ok := false
	defer func() {
		if !ok {
			lock.Close()
		}
	}()
	keyPath := filepath.Join(dir, "key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(filepath.Join(dir, "state.enc")); !errors.Is(statErr, os.ErrNotExist) {
			return nil, errors.New("기존 상태를 복원하려면 원래 암호화 키가 필요합니다")
		}
		keyBytes := credential.NewToken("")
		raw, _ := base64.RawURLEncoding.DecodeString(keyBytes)
		key = []byte(base64.StdEncoding.EncodeToString(raw))
		err = os.WriteFile(keyPath, key, 0600)
	}
	if err != nil {
		return nil, errors.New("로컬 암호화 키를 준비할 수 없습니다")
	}
	s.cipher, err = credential.NewCipher(string(key))
	if err != nil {
		return nil, errors.New("앱 암호화 키를 확인하세요")
	}

	// 2. 저장된 연결·이력·발송 대기를 검증한 뒤 복원한다.
	s.data = state{Version: 1, Servers: []dockerconn.Server{}, Destinations: []Destination{{ID: "desktop", Name: "이 PC 알림", Kind: "desktop", Enabled: true}}, Events: []Record{}, Jobs: []job{}, Cursors: map[string]int64{}, Preferences: Preferences{Items: []Preference{}}}
	data, err := os.ReadFile(filepath.Join(dir, "state.enc"))
	if err == nil {
		plain, decodeErr := s.cipher.Open(data, "local-state-v1")
		if decodeErr != nil || json.Unmarshal(plain, &s.data) != nil || s.data.Version != 1 || s.data.Cursors == nil {
			return nil, errors.New("로컬 상태를 복원할 수 없습니다. 데이터와 암호화 키를 확인하세요")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("로컬 상태 파일을 읽을 수 없습니다")
	}
	ok = true
	return s, nil
}

// snapshot은 호출자가 내부 슬라이스나 설정을 바꿀 수 없는 복사본을 반환한다.
func (s *diskStore) snapshot() state {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.data)
}

// cloneState는 맵·슬라이스·자격 증명을 포함하는 상태를 깊게 복사한다.
func cloneState(value state) state {
	data, _ := json.Marshal(value)
	var result state
	_ = json.Unmarshal(data, &result)
	return result
}

// update는 디스크 저장이 성공한 경우에만 메모리의 상태를 교체한다.
func (s *diskStore) update(change func(*state) error) error {
	// 1. 기존 상태의 복사본에 변경을 적용하고 암호화한다.
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.data)
	if err := change(&next); err != nil {
		return err
	}
	plain, err := json.Marshal(next)
	if err != nil {
		return err
	}
	data := s.cipher.Seal(plain, "local-state-v1")
	file, err := os.CreateTemp(s.dir, ".state-")
	if err != nil {
		return errors.New("로컬 상태를 저장할 수 없습니다")
	}
	defer os.Remove(file.Name())
	defer file.Close()

	// 2. 완료된 파일만 원자적으로 교체해 중간 저장 내용을 읽지 않게 한다.
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), filepath.Join(s.dir, "state.enc"))
	}
	if err != nil {
		return errors.New("로컬 상태를 저장할 수 없습니다")
	}
	s.data = next
	return nil
}

// selected는 이벤트별 예외를 먼저 적용하고 없으면 개인 기본값을 사용한다.
func selected(p Preferences, key string) bool {
	for _, item := range p.Items {
		if item.EventKey == key {
			return item.Enabled
		}
	}
	return p.DefaultEnabled
}
