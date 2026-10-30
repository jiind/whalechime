package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// GetPreferences는 본인의 기본 수신값과 세부 항목을 조회한다.
func (s *Store) GetPreferences(ctx context.Context, userID string) (Preferences, error) {
	// 1. 개인 기본값을 확인한다.
	var prefs Preferences
	if err := s.Pool.QueryRow(ctx, "SELECT default_enabled FROM users WHERE id=$1", userID).Scan(&prefs.DefaultEnabled); err != nil {
		return prefs, notFound(err)
	}

	// 2. 명시적으로 선택한 항목만 반환한다.
	rows, err := s.Pool.Query(ctx, "SELECT event_key,enabled FROM preferences WHERE user_id=$1 ORDER BY event_key", userID)
	if err != nil {
		return prefs, err
	}
	prefs.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Preference])
	return prefs, err
}

// SetPreferences는 수신처와 무관하게 개인 수신 항목 전체를 교체한다.
func (s *Store) SetPreferences(ctx context.Context, userID string, prefs Preferences) error {
	// 1. 기본값과 항목 변경을 하나의 트랜잭션으로 시작한다.
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE users SET default_enabled=$2 WHERE id=$1", userID, prefs.DefaultEnabled); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM preferences WHERE user_id=$1", userID); err != nil {
		return err
	}

	// 2. 사용자가 전달한 예외 항목을 저장한다.
	for _, item := range prefs.Items {
		if _, err = tx.Exec(ctx, "INSERT INTO preferences(user_id,event_key,enabled) VALUES ($1,$2,$3)", userID, item.EventKey, item.Enabled); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListDestinations는 자격 증명 없이 본인의 수신처 목록을 반환한다.
func (s *Store) ListDestinations(ctx context.Context, userID string) ([]Destination, error) {
	// 1. 수신처의 공개 필드만 조회한다.
	rows, err := s.Pool.Query(ctx, "SELECT id,name,kind,enabled FROM destinations WHERE user_id=$1 ORDER BY created_at,id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 2. 빈 목록도 JSON 배열로 반환할 수 있게 초기화한다.
	items := make([]Destination, 0)
	for rows.Next() {
		var item Destination
		if err := rows.Scan(&item.ID, &item.Name, &item.Kind, &item.Enabled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SaveDestination은 본인의 수신처만 추가하거나 설정 전체를 갱신한다.
func (s *Store) SaveDestination(ctx context.Context, userID string, d Destination, create bool) error {
	if create {
		_, err := s.Pool.Exec(ctx, `INSERT INTO destinations(id,user_id,name,kind,enabled,encrypted_config)
			VALUES ($1,$2,$3,$4,$5,$6)`, d.ID, userID, d.Name, d.Kind, d.Enabled, d.EncryptedConfig)
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE destinations SET name=$3,kind=$4,enabled=$5,encrypted_config=$6
		WHERE id=$1 AND user_id=$2`, d.ID, userID, d.Name, d.Kind, d.Enabled, d.EncryptedConfig)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// GetDestination은 소유자 조건을 포함해 테스트 발송용 설정을 조회한다.
func (s *Store) GetDestination(ctx context.Context, userID, id string) (Destination, error) {
	var d Destination
	err := s.Pool.QueryRow(ctx, `SELECT id,name,kind,enabled,encrypted_config FROM destinations
		WHERE id=$1 AND user_id=$2`, id, userID).Scan(&d.ID, &d.Name, &d.Kind, &d.Enabled, &d.EncryptedConfig)
	return d, notFound(err)
}

// DeleteDestination은 본인 수신처와 연결된 발송 대기를 삭제한다.
func (s *Store) DeleteDestination(ctx context.Context, userID, id string) error {
	tag, err := s.Pool.Exec(ctx, "DELETE FROM destinations WHERE id=$1 AND user_id=$2", id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// EnableDestination은 연결 정보를 다시 입력하지 않고 본인 수신처만 켜거나 끈다.
func (s *Store) EnableDestination(ctx context.Context, userID, id string, enabled bool) error {
	tag, err := s.Pool.Exec(ctx, "UPDATE destinations SET enabled=$3 WHERE id=$1 AND user_id=$2", id, userID, enabled)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
