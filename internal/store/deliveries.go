package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jiind/whalechime/internal/credential"
	"github.com/jiind/whalechime/internal/event"
)

// Delivery는 만료 가능한 임대와 발송 직전 권한 확인 결과를 가진 작업이다.
type Delivery struct {
	ID          int64
	LeaseToken  string
	Attempts    int
	Destination Destination
	ServerName  string
	Event       event.Event
	Allowed     bool
}

// ClaimDelivery는 잠긴 작업을 건너뛰고 만료된 작업을 다시 임대한다.
func (s *Store) ClaimDelivery(ctx context.Context) (Delivery, error) {
	// 1. 트랜잭션에서 작업 하나를 임대하고 작업자 식별자를 발급한다.
	var job Delivery
	job.LeaseToken = credential.NewToken("lease_")
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return job, err
	}
	defer tx.Rollback(ctx)
	var eventID, destinationID string
	err = tx.QueryRow(ctx, `WITH next AS (
		SELECT id FROM deliveries
		WHERE (status='pending' AND available_at <= now())
		OR (status='processing' AND lease_until <= now())
		ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE deliveries d SET status='processing', attempts=attempts+1,
		lease_until=now()+interval '60 seconds',lease_token=$1
		FROM next WHERE d.id=next.id RETURNING d.id,d.event_id,d.destination_id,d.attempts`, job.LeaseToken).
		Scan(&job.ID, &eventID, &destinationID, &job.Attempts)
	if err != nil {
		return job, notFound(err)
	}

	// 2. 변경된 수신처, 서버 연결 권한, 개인 수신 항목을 다시 확인한다.
	err = tx.QueryRow(ctx, `SELECT d.id,d.name,d.kind,d.enabled,d.encrypted_config,s.name,e.payload,
		d.enabled AND EXISTS(SELECT 1 FROM server_user_access m WHERE m.user_id=d.user_id AND m.server_id=e.server_id)
		AND COALESCE(p.enabled,u.default_enabled)
		FROM destinations d JOIN users u ON u.id=d.user_id
		JOIN events e ON e.id=$1 JOIN servers s ON s.id=e.server_id
		LEFT JOIN preferences p ON p.user_id=u.id AND p.event_key=e.event_key WHERE d.id=$2`, eventID, destinationID).
		Scan(&job.Destination.ID, &job.Destination.Name, &job.Destination.Kind, &job.Destination.Enabled,
			&job.Destination.EncryptedConfig, &job.ServerName, &job.Event, &job.Allowed)
	if err != nil {
		return job, err
	}
	return job, tx.Commit(ctx)
}

// FinishDelivery는 현재 임대 소유자만 성공·실패·재시도 상태를 확정하게 한다.
func (s *Store) FinishDelivery(ctx context.Context, job Delivery, status, message string, delay time.Duration) error {
	_, err := s.Pool.Exec(ctx, `UPDATE deliveries SET status=$3,last_error=NULLIF($4,''),
		available_at=now()+$5::double precision*interval '1 second',lease_until=NULL,lease_token=NULL,
		sent_at=CASE WHEN $3='sent' THEN now() ELSE sent_at END WHERE id=$1 AND lease_token=$2`,
		job.ID, job.LeaseToken, status, message, delay.Seconds())
	return err
}

// DeliveryStatus는 자격 증명을 포함하지 않는 개인 발송 이력이다.
type DeliveryStatus struct {
	ID              int64      `json:"id"`
	EventID         string     `json:"event_id"`
	DestinationID   string     `json:"destination_id"`
	DestinationName string     `json:"destination_name"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	LastError       *string    `json:"last_error"`
	SentAt          *time.Time `json:"sent_at"`
}

// ListDeliveries는 본인 수신처의 발송 상태만 최근 순서로 조회한다.
func (s *Store) ListDeliveries(ctx context.Context, userID string, limit int) ([]DeliveryStatus, error) {
	rows, err := s.Pool.Query(ctx, `SELECT j.id,j.event_id,d.id,d.name,j.status,j.attempts,j.last_error,j.sent_at
		FROM deliveries j JOIN destinations d ON d.id=j.destination_id WHERE d.user_id=$1
		ORDER BY j.id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DeliveryStatus])
}
