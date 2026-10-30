-- 사용자 수신 항목과 수신처를 독립적으로 보관한다.
CREATE TABLE users (
    id text PRIMARY KEY,
    name text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    is_admin boolean NOT NULL DEFAULT false,
    default_enabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE servers (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    token_hash text NOT NULL UNIQUE,
    last_seen_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE server_members (
    server_id text NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (server_id, user_id)
);

CREATE TABLE preferences (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_key text NOT NULL,
    enabled boolean NOT NULL,
    PRIMARY KEY (user_id, event_key)
);

CREATE TABLE destinations (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    kind text NOT NULL,
    encrypted_config bytea NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

CREATE TABLE events (
    id text PRIMARY KEY,
    server_id text NOT NULL REFERENCES servers(id),
    event_key text NOT NULL,
    occurred_at timestamptz NOT NULL,
    payload jsonb NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_server_time_idx ON events(server_id, received_at DESC, id);

-- 발송 대기는 이벤트와 같은 트랜잭션에서 생성하며 수신처별로 재시도한다.
CREATE TABLE deliveries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id text NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    destination_id text NOT NULL REFERENCES destinations(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'sent', 'failed', 'cancelled')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    lease_token text,
    last_error text,
    sent_at timestamptz,
    UNIQUE (event_id, destination_id)
);
CREATE INDEX deliveries_pending_idx ON deliveries(available_at) WHERE status = 'pending';
CREATE INDEX deliveries_lease_idx ON deliveries(lease_until) WHERE status = 'processing';
