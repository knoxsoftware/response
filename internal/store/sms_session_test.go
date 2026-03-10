package store_test

// Integration tests — require DATABASE_URL env var pointing to a test DB.
// Run with: DATABASE_URL=postgres://... go test ./internal/store/... -run TestSMSSession

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mattventura/respond/internal/store"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestSMSSessionUpsertAndGet(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM sms_sessions WHERE phone_number = '+15005550001'")

	s := store.NewSMSSessionStore(pool)

	if err := s.Upsert(ctx, "+15005550001", "node_a"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	node, err := s.GetNode(ctx, "+15005550001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if node != "node_a" {
		t.Errorf("expected node_a, got %s", node)
	}

	if err := s.Upsert(ctx, "+15005550001", "node_b"); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	node, err = s.GetNode(ctx, "+15005550001")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if node != "node_b" {
		t.Errorf("expected node_b after update, got %s", node)
	}
}

func TestSMSSessionDelete(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM sms_sessions WHERE phone_number = '+15005550002'")

	s := store.NewSMSSessionStore(pool)
	s.Upsert(ctx, "+15005550002", "root")
	s.Delete(ctx, "+15005550002")

	_, err := s.GetNode(ctx, "+15005550002")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestSMSSessionDeleteExpired(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM sms_sessions WHERE phone_number IN ('+15005550003', '+15005550004')")

	s := store.NewSMSSessionStore(pool)
	s.Upsert(ctx, "+15005550003", "root")
	s.Upsert(ctx, "+15005550004", "root")

	pool.Exec(ctx, "UPDATE sms_sessions SET last_activity = NOW() - INTERVAL '2 hours' WHERE phone_number = '+15005550003'")

	deleted, err := s.DeleteExpired(ctx, 30*time.Minute)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	node, err := s.GetNode(ctx, "+15005550004")
	if err != nil || node != "root" {
		t.Errorf("active session should remain: node=%s err=%v", node, err)
	}
}
