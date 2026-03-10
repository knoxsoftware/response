package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SMSSessionStore struct{ db *pgxpool.Pool }

func NewSMSSessionStore(db *pgxpool.Pool) *SMSSessionStore {
	return &SMSSessionStore{db: db}
}

func (s *SMSSessionStore) GetNode(ctx context.Context, phone string) (string, error) {
	var node string
	err := s.db.QueryRow(ctx,
		`SELECT current_node FROM sms_sessions WHERE phone_number=$1`, phone,
	).Scan(&node)
	if err != nil {
		return "", fmt.Errorf("get sms session: %w", err)
	}
	return node, nil
}

func (s *SMSSessionStore) Upsert(ctx context.Context, phone, node string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO sms_sessions (phone_number, current_node, last_activity)
		VALUES ($1, $2, NOW())
		ON CONFLICT (phone_number) DO UPDATE
		SET current_node=$2, last_activity=NOW()
	`, phone, node)
	return err
}

func (s *SMSSessionStore) Delete(ctx context.Context, phone string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sms_sessions WHERE phone_number=$1`, phone)
	return err
}

// DeleteExpired removes sessions inactive for longer than timeout. Returns count deleted.
func (s *SMSSessionStore) DeleteExpired(ctx context.Context, timeout time.Duration) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM sms_sessions WHERE last_activity < NOW() - $1::interval`,
		fmt.Sprintf("%d seconds", int(timeout.Seconds())),
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
