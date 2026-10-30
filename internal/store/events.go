package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jiind/whalechime/internal/event"
)

// StoredEvent는 원본 이벤트 내용과 수집 서버를 함께 표현한다.
type StoredEvent struct {
	ID         string      `json:"id"`
	ServerID   string      `json:"server_id"`
	EventKey   string      `json:"event_key"`
	OccurredAt time.Time   `json:"occurred_at"`
	Payload    event.Event `json:"payload"`
}

// Ingest는 이벤트 기록과 선택된 모든 개인 수신처의 발송 대기를 함께 저장한다.
func (s *Store) Ingest(ctx context.Context, serverID string, e event.Event) (string, bool, error) {
	// 1. 재전송에도 같은 식별자를 사용해 이벤트를 한 번만 저장한다.
	id := e.ID(serverID)
	payload, err := json.Marshal(e)
	if err != nil {
		return "", false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO events(id,server_id,event_key,occurred_at,payload)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, id, serverID, e.Key(), e.OccurredAt(), payload)
	if err != nil {
		return "", false, err
	}
	created := tag.RowsAffected() == 1

	// 2. 관련 사용자 중 해당 항목을 켠 사람의 활성 수신처 전체를 선택한다.
	if created {
		_, err = tx.Exec(ctx, `INSERT INTO deliveries(event_id,destination_id)
			SELECT $1,d.id FROM server_user_access m
			JOIN users u ON u.id=m.user_id
			JOIN destinations d ON d.user_id=u.id AND d.enabled
			LEFT JOIN preferences p ON p.user_id=u.id AND p.event_key=$3
			WHERE m.server_id=$2 AND COALESCE(p.enabled,u.default_enabled)
			ON CONFLICT DO NOTHING`, id, serverID, e.Key())
		if err != nil {
			return "", false, err
		}
	}

	// 3. 서버 연결 상태도 기록한 뒤 모든 변경을 확정한다.
	if _, err = tx.Exec(ctx, "UPDATE servers SET last_seen_at=now() WHERE id=$1", serverID); err != nil {
		return "", false, err
	}
	return id, created, tx.Commit(ctx)
}

// ListEvents는 사용자 관련 서버의 이벤트를 수신 설정과 무관하게 조회한다.
func (s *Store) ListEvents(ctx context.Context, userID, serverID, eventKey string, limit int) ([]StoredEvent, error) {
	rows, err := s.Pool.Query(ctx, `SELECT e.id,e.server_id,e.event_key,e.occurred_at,e.payload
		FROM events e JOIN server_user_access m ON m.server_id=e.server_id
		WHERE m.user_id=$1 AND ($2='' OR e.server_id=$2) AND ($3='' OR e.event_key=$3)
		ORDER BY e.received_at DESC,e.id LIMIT $4`, userID, serverID, eventKey, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[StoredEvent])
}

// EventKeys는 기본 목록과 사용자 관련 서버에서 실제 관찰한 새 항목을 합친다.
func (s *Store) EventKeys(ctx context.Context, userID string) ([]string, error) {
	// 1. 알려진 이벤트와 관찰된 이벤트를 모두 모은다.
	set := make(map[string]bool)
	for _, key := range event.KnownKeys() {
		set[key] = true
	}
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT e.event_key FROM events e
		JOIN server_user_access m ON m.server_id=e.server_id WHERE m.user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		set[key] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. 개인 설정 화면에서 안정적으로 표시하도록 정렬한다.
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}
