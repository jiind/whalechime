package event

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Event는 Docker 이벤트를 종류 제한 없이 전달하는 공통 계약이다.
type Event struct {
	Type       string            `json:"type"`
	Action     string            `json:"action"`
	ActorID    string            `json:"actor_id"`
	Scope      string            `json:"scope"`
	TimeNano   int64             `json:"time_nano"`
	Attributes map[string]string `json:"attributes"`
}

// Validate는 필수 식별값과 발생 시각의 형식을 확인한다.
func (e Event) Validate() error {
	if e.Type == "" || e.Action == "" || len(e.Type) > 128 || len(e.Action) > 4096 || e.TimeNano <= 0 {
		return errors.New("이벤트 종류, 동작, 발생 시각을 확인하세요")
	}
	return nil
}

// Key는 명령어나 상태가 붙은 동작을 안정적인 개인 수신 항목으로 정규화한다.
func (e Event) Key() string {
	action, _, _ := strings.Cut(e.Action, ":")
	return e.Type + "." + strings.TrimSpace(action)
}

// ID는 서버와 이벤트 내용으로 재전송 시에도 동일한 식별자를 계산한다.
func (e Event) ID(serverID string) string {
	data, _ := json.Marshal(e)
	sum := sha256.Sum256(append([]byte(serverID+"\n"), data...))
	return hex.EncodeToString(sum[:])
}

// OccurredAt은 Docker의 나노초 시각을 UTC 시각으로 변환한다.
func (e Event) OccurredAt() time.Time {
	return time.Unix(0, e.TimeNano).UTC()
}

// KnownKeys는 최초 설정에 사용할 Docker 이벤트 목록을 반환한다.
func KnownKeys() []string {
	groups := map[string]string{
		"container": "attach commit copy create destroy detach die exec_create exec_detach exec_die exec_start export health_status kill oom pause rename resize restart start stop top unpause update prune",
		"image":     "create delete import load pull push save tag untag prune",
		"network":   "create connect destroy disconnect remove update prune",
		"volume":    "create destroy mount unmount prune",
		"daemon":    "reload",
		"plugin":    "enable disable install remove",
		"service":   "create remove update",
		"node":      "create remove update",
		"secret":    "create remove update",
		"config":    "create remove update",
	}
	var keys []string
	for kind, actions := range groups {
		for _, action := range strings.Fields(actions) {
			keys = append(keys, kind+"."+action)
		}
	}
	return keys
}
