package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jiind/whalechime/internal/credential"
)

// BootstrapAdmin은 환경변수의 관리자 토큰을 설치 계정에 적용한다.
func (s *Store) BootstrapAdmin(ctx context.Context, token string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO users(id, name, token_hash, is_admin)
		VALUES ('bootstrap-admin', '관리자', $1, true)
		ON CONFLICT (id) DO UPDATE SET token_hash = EXCLUDED.token_hash`, credential.Hash(token))
	return err
}

// AuthenticateUser는 토큰 해시로 사용자 계정을 조회한다.
func (s *Store) AuthenticateUser(ctx context.Context, token string) (User, error) {
	var user User
	err := s.Pool.QueryRow(ctx, "SELECT id, name, is_admin FROM users WHERE token_hash=$1", credential.Hash(token)).Scan(&user.ID, &user.Name, &user.IsAdmin)
	return user, notFound(err)
}

// AuthenticateAgent는 서버 전용 토큰으로 이벤트의 소유 서버를 결정한다.
func (s *Store) AuthenticateAgent(ctx context.Context, token string) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, "SELECT id FROM servers WHERE token_hash=$1", credential.Hash(token)).Scan(&id)
	return id, notFound(err)
}

// CreateUser는 개인 설정용 계정을 만들고 토큰을 한 번만 반환한다.
func (s *Store) CreateUser(ctx context.Context, name string) (User, string, error) {
	user := User{ID: credential.NewToken("user_"), Name: name}
	token := credential.NewToken("wc_user_")
	_, err := s.Pool.Exec(ctx, "INSERT INTO users(id,name,token_hash) VALUES ($1,$2,$3)", user.ID, name, credential.Hash(token))
	return user, token, err
}

// CreateServer는 서버를 등록하고 등록자를 관련 사용자로 연결한다.
func (s *Store) CreateServer(ctx context.Context, name, userID string) (Server, string, error) {
	// 1. 서버 식별자와 전용 인증 토큰을 준비한다.
	server := Server{ID: credential.NewToken("server_"), Name: name}
	token := credential.NewToken("wc_agent_")
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return server, "", err
	}
	defer tx.Rollback(ctx)

	// 2. 서버와 초기 관련 사용자를 함께 저장한다.
	if _, err = tx.Exec(ctx, "INSERT INTO servers(id,name,token_hash) VALUES ($1,$2,$3)", server.ID, name, credential.Hash(token)); err != nil {
		return server, "", err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO server_members(server_id,user_id) VALUES ($1,$2)", server.ID, userID); err != nil {
		return server, "", err
	}
	return server, token, tx.Commit(ctx)
}

// SetMember는 관리자가 서버 관련 사용자를 추가하거나 해제한다.
func (s *Store) SetMember(ctx context.Context, serverID, userID string, enabled bool) error {
	if enabled {
		_, err := s.Pool.Exec(ctx, "INSERT INTO server_members(server_id,user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING", serverID, userID)
		return err
	}
	_, err := s.Pool.Exec(ctx, "DELETE FROM server_members WHERE server_id=$1 AND user_id=$2", serverID, userID)
	return err
}

// ListServers는 사용자에게 연결된 서버만 조회한다.
func (s *Store) ListServers(ctx context.Context, userID string) ([]Server, error) {
	rows, err := s.Pool.Query(ctx, `SELECT s.id,s.name,s.last_seen_at FROM servers s
		JOIN server_user_access m ON m.server_id=s.id WHERE m.user_id=$1 ORDER BY s.name`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Server])
}

// Heartbeat는 이벤트가 없는 서버도 마지막 연결 시각을 갱신한다.
func (s *Store) Heartbeat(ctx context.Context, serverID string) error {
	_, err := s.Pool.Exec(ctx, "UPDATE servers SET last_seen_at=now() WHERE id=$1", serverID)
	return err
}

// ListUsers는 관리자가 서버 연결에 사용할 계정 목록을 조회한다.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id,name,is_admin FROM users ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[User])
}

// ListAllServers는 관리자가 감시 서버 전체를 조회한다.
func (s *Store) ListAllServers(ctx context.Context) ([]Server, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id,name,last_seen_at FROM servers ORDER BY name")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Server])
}

// ListMembers는 특정 서버의 관련 사용자를 조회한다.
func (s *Store) ListMembers(ctx context.Context, serverID string) ([]User, error) {
	rows, err := s.Pool.Query(ctx, `SELECT u.id,u.name,u.is_admin FROM users u
		JOIN server_members m ON m.user_id=u.id WHERE m.server_id=$1 ORDER BY u.name`, serverID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[User])
}

// RotateUserToken은 본인 또는 관리자가 계정의 이전 토큰을 폐기하게 한다.
func (s *Store) RotateUserToken(ctx context.Context, userID string) (string, error) {
	token := credential.NewToken("wc_user_")
	tag, err := s.Pool.Exec(ctx, "UPDATE users SET token_hash=$2 WHERE id=$1 AND NOT is_admin", userID, credential.Hash(token))
	if err == nil && tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return token, err
}
