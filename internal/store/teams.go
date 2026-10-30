package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jiind/whalechime/internal/credential"
)

// Team은 여러 사용자를 서버에 함께 연결할 그룹이다.
type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateTeam은 고유한 이름으로 사용자 팀을 등록한다.
func (s *Store) CreateTeam(ctx context.Context, name string) (Team, error) {
	team := Team{ID: credential.NewToken("team_"), Name: name}
	_, err := s.Pool.Exec(ctx, "INSERT INTO teams(id,name) VALUES ($1,$2)", team.ID, team.Name)
	return team, err
}

// ListTeams는 관리자가 연결할 팀 목록을 조회한다.
func (s *Store) ListTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id,name FROM teams ORDER BY name")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Team])
}

// DeleteTeam은 팀의 사용자·서버 연결을 함께 해제한다.
func (s *Store) DeleteTeam(ctx context.Context, teamID string) error {
	tag, err := s.Pool.Exec(ctx, "DELETE FROM teams WHERE id=$1", teamID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetTeamMember는 관리자가 사용자를 팀에 추가하거나 제거하게 한다.
func (s *Store) SetTeamMember(ctx context.Context, teamID, userID string, enabled bool) error {
	if enabled {
		_, err := s.Pool.Exec(ctx, "INSERT INTO team_members(team_id,user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING", teamID, userID)
		return err
	}
	_, err := s.Pool.Exec(ctx, "DELETE FROM team_members WHERE team_id=$1 AND user_id=$2", teamID, userID)
	return err
}

// ListTeamMembers는 해당 팀의 현재 구성원을 조회한다.
func (s *Store) ListTeamMembers(ctx context.Context, teamID string) ([]User, error) {
	rows, err := s.Pool.Query(ctx, `SELECT u.id,u.name,u.is_admin FROM users u
		JOIN team_members m ON m.user_id=u.id WHERE m.team_id=$1 ORDER BY u.name`, teamID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[User])
}

// SetServerTeam은 서버에 연결할 담당 팀을 지정하거나 해제한다.
func (s *Store) SetServerTeam(ctx context.Context, serverID, teamID string, enabled bool) error {
	if enabled {
		_, err := s.Pool.Exec(ctx, "INSERT INTO server_teams(server_id,team_id) VALUES ($1,$2) ON CONFLICT DO NOTHING", serverID, teamID)
		return err
	}
	_, err := s.Pool.Exec(ctx, "DELETE FROM server_teams WHERE server_id=$1 AND team_id=$2", serverID, teamID)
	return err
}

// ListServerTeams는 서버에 연결된 담당 팀을 조회한다.
func (s *Store) ListServerTeams(ctx context.Context, serverID string) ([]Team, error) {
	rows, err := s.Pool.Query(ctx, `SELECT t.id,t.name FROM teams t
		JOIN server_teams s ON s.team_id=t.id WHERE s.server_id=$1 ORDER BY t.name`, serverID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Team])
}
