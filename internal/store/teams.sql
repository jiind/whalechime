-- 직접 연결과 팀 연결을 하나의 접근 범위로 조회한다.
CREATE TABLE teams (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE
);
CREATE TABLE team_members (
    team_id text NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, user_id)
);
CREATE TABLE server_teams (
    server_id text NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    team_id text NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    PRIMARY KEY (server_id, team_id)
);
CREATE VIEW server_user_access AS
    SELECT server_id, user_id FROM server_members
    UNION
    SELECT s.server_id, m.user_id FROM server_teams s
    JOIN team_members m ON m.team_id = s.team_id;
