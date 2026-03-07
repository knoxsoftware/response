package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionState struct {
	Step    string            `json:"step"`
	Pending map[string]string `json:"pending,omitempty"`
}

type Session struct {
	CallSid string
	Caller  string
	State   SessionState
}

type SessionStore struct{ db *pgxpool.Pool }

func NewSessionStore(db *pgxpool.Pool) *SessionStore { return &SessionStore{db: db} }

func (s *SessionStore) Get(ctx context.Context, callSid string) (*Session, error) {
	var raw []byte
	sess := &Session{CallSid: callSid}
	err := s.db.QueryRow(ctx,
		`SELECT caller, session_state FROM call_sessions WHERE call_sid=$1`, callSid,
	).Scan(&sess.Caller, &raw)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if err := json.Unmarshal(raw, &sess.State); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return sess, nil
}

func (s *SessionStore) Upsert(ctx context.Context, sess *Session) error {
	raw, err := json.Marshal(sess.State)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO call_sessions (call_sid, caller, session_state)
		VALUES ($1, $2, $3)
		ON CONFLICT (call_sid) DO UPDATE
		SET session_state=$3
	`, sess.CallSid, sess.Caller, raw)
	return err
}

func (s *SessionStore) Delete(ctx context.Context, callSid string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM call_sessions WHERE call_sid=$1`, callSid)
	return err
}
