package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

//go:embed teams.sql
var teamsSchema string

// ErrNotFound는 접근 가능한 데이터가 없음을 나타낸다.
var ErrNotFound = errors.New("대상을 찾을 수 없습니다")

// Store는 PostgreSQL 조회와 트랜잭션 경계를 관리한다.
type Store struct {
	Pool *pgxpool.Pool
}

// Open은 연결을 확인하고 초기 스키마를 적용한다.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	// 1. 연결 풀과 실제 DB 연결을 확인한다.
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	s := &Store{Pool: pool}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	// 2. 여러 서버가 시작되어도 마이그레이션을 한 번씩만 적용한다.
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// migrate는 트랜잭션 잠금으로 초기 스키마 설치를 직렬화한다.
func (s *Store) migrate(ctx context.Context) error {
	// 1. 마이그레이션 전용 트랜잭션과 잠금을 획득한다.
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(824719023)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}

	// 2. 설치되지 않은 스키마만 적용하고 이력을 함께 저장한다.
	var version int
	if err = tx.QueryRow(ctx, "SELECT COALESCE(max(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return err
	}
	migrations := []string{schema, teamsSchema}
	if version > len(migrations) {
		return fmt.Errorf("지원하지 않는 DB 스키마 버전: %d", version)
	}
	for next := version; next < len(migrations); next++ {
		if _, err = tx.Exec(ctx, migrations[next]); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES ($1)", next+1); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// notFound는 드라이버의 빈 조회 오류를 저장소 공통 오류로 변환한다.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Server는 자격 증명을 제외한 감시 서버 정보다.
type Server struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

// User는 사용자 본인 또는 관리자에게 제공할 계정 정보다.
type User struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
}

// Destination은 자격 증명과 공개 수신처 메타데이터를 분리한다.
type Destination struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Enabled         bool   `json:"enabled"`
	EncryptedConfig []byte `json:"-"`
}

// Preference는 특정 이벤트의 개인 수신 여부다.
type Preference struct {
	EventKey string `json:"event_key"`
	Enabled  bool   `json:"enabled"`
}

// Preferences는 미지정 이벤트 기본값과 명시적 선택을 함께 표현한다.
type Preferences struct {
	DefaultEnabled bool         `json:"default_enabled"`
	Items          []Preference `json:"items"`
}
