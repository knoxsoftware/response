package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Responder struct {
	ID          string
	PhoneNumber string
	Available   bool
	IsValidated bool
	IsAdmin     bool
	PinHash     *string
}

func (r *Responder) VerifyPIN(pin string) bool {
	if r.PinHash == nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(*r.PinHash), []byte(pin)) == nil
}

type ResponderStore struct{ db *pgxpool.Pool }

func NewResponderStore(db *pgxpool.Pool) *ResponderStore { return &ResponderStore{db: db} }

func (s *ResponderStore) FindByPhone(ctx context.Context, phone string) (*Responder, error) {
	r := &Responder{}
	err := s.db.QueryRow(ctx,
		`SELECT id, phone_number, available, is_validated, is_admin, pin_hash FROM responders WHERE phone_number=$1`, phone,
	).Scan(&r.ID, &r.PhoneNumber, &r.Available, &r.IsValidated, &r.IsAdmin, &r.PinHash)
	if err != nil {
		return nil, fmt.Errorf("find responder: %w", err)
	}
	return r, nil
}

func (s *ResponderStore) ListAvailable(ctx context.Context) ([]Responder, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, phone_number, available, is_validated, is_admin, pin_hash FROM responders WHERE available=TRUE AND is_validated=TRUE ORDER BY phone_number`,
	)
	if err != nil {
		return nil, fmt.Errorf("list available: %w", err)
	}
	defer rows.Close()
	var out []Responder
	for rows.Next() {
		var r Responder
		if err := rows.Scan(&r.ID, &r.PhoneNumber, &r.Available, &r.IsValidated, &r.IsAdmin, &r.PinHash); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *ResponderStore) ListAll(ctx context.Context) ([]Responder, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, phone_number, available, is_validated, is_admin, pin_hash FROM responders ORDER BY phone_number`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all: %w", err)
	}
	defer rows.Close()
	var out []Responder
	for rows.Next() {
		var r Responder
		if err := rows.Scan(&r.ID, &r.PhoneNumber, &r.Available, &r.IsValidated, &r.IsAdmin, &r.PinHash); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *ResponderStore) Create(ctx context.Context, phone string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO responders (phone_number) VALUES ($1)`, phone,
	)
	return err
}

func (s *ResponderStore) Delete(ctx context.Context, phone string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM responders WHERE phone_number=$1`, phone)
	return err
}

func (s *ResponderStore) SetAvailable(ctx context.Context, phone string, available bool) error {
	_, err := s.db.Exec(ctx,
		`UPDATE responders SET available=$1 WHERE phone_number=$2`, available, phone,
	)
	return err
}

func (s *ResponderStore) CountByAvailability(ctx context.Context) (active, inactive int, err error) {
	rows, err := s.db.Query(ctx, `SELECT available, COUNT(*) FROM responders GROUP BY available`)
	if err != nil {
		return 0, 0, fmt.Errorf("count by availability: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var avail bool
		var n int
		if err := rows.Scan(&avail, &n); err != nil {
			return 0, 0, err
		}
		if avail {
			active = n
		} else {
			inactive = n
		}
	}
	return active, inactive, rows.Err()
}

func (s *ResponderStore) SetValidated(ctx context.Context, phone string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE responders SET is_validated=TRUE WHERE phone_number=$1`, phone,
	)
	return err
}

func (s *ResponderStore) SetPIN(ctx context.Context, phone, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash pin: %w", err)
	}
	_, err = s.db.Exec(ctx, `UPDATE responders SET pin_hash=$1 WHERE phone_number=$2`, string(hash), phone)
	return err
}

func (s *ResponderStore) UpdatePIN(ctx context.Context, phone, pin string) error {
	return s.SetPIN(ctx, phone, pin)
}

func (s *ResponderStore) ToggleAvailable(ctx context.Context, phone string) (bool, error) {
	var newState bool
	err := s.db.QueryRow(ctx,
		`UPDATE responders SET available=NOT available WHERE phone_number=$1 RETURNING available`, phone,
	).Scan(&newState)
	return newState, err
}

func (s *ResponderStore) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM responders WHERE is_admin=TRUE`).Scan(&n)
	return n, err
}

func (s *ResponderStore) CreateAdmin(ctx context.Context, phone, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash pin: %w", err)
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO responders (phone_number, is_admin, pin_hash) VALUES ($1, TRUE, $2)`,
		phone, string(hash),
	)
	return err
}

func (s *ResponderStore) SetAdmin(ctx context.Context, phone string, isAdmin bool) error {
	_, err := s.db.Exec(ctx,
		`UPDATE responders SET is_admin=$1 WHERE phone_number=$2`, isAdmin, phone,
	)
	return err
}
