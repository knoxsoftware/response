package sms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mattventura/respond/internal/sms"
)

type mockSMSStore struct {
	nodes map[string]string
}

func (m *mockSMSStore) GetNode(ctx context.Context, phone string) (string, error) {
	if n, ok := m.nodes[phone]; ok {
		return n, nil
	}
	return "", sms.ErrSessionNotFound
}
func (m *mockSMSStore) Upsert(ctx context.Context, phone, node string) error {
	m.nodes[phone] = node
	return nil
}
func (m *mockSMSStore) Delete(ctx context.Context, phone string) error {
	delete(m.nodes, phone)
	return nil
}

func newTestEngine(t *testing.T) *sms.Engine {
	t.Helper()
	tree, err := sms.LoadTree(strings.NewReader(validYAML))
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}
	store := &mockSMSStore{nodes: map[string]string{}}
	return sms.NewEngine(tree, store)
}

func TestEngine_NewSession(t *testing.T) {
	e := newTestEngine(t)
	resp, err := e.Handle(context.Background(), "+15005550001", "hi")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !strings.Contains(resp.Message, "Press 1 for A") {
		t.Errorf("expected root prompt, got: %s", resp.Message)
	}
}

func TestEngine_ValidOption(t *testing.T) {
	e := newTestEngine(t)
	e.Handle(context.Background(), "+15005550001", "hi")
	resp, err := e.Handle(context.Background(), "+15005550001", "1")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp.Message != "You chose A." {
		t.Errorf("expected terminal response, got: %s", resp.Message)
	}
	if !resp.Terminal {
		t.Error("expected terminal=true")
	}
}

func TestEngine_InvalidOption(t *testing.T) {
	e := newTestEngine(t)
	e.Handle(context.Background(), "+15005550001", "hi")
	resp, err := e.Handle(context.Background(), "+15005550001", "9")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !strings.Contains(resp.Message, "Press 1 for A") {
		t.Errorf("expected repeated prompt, got: %s", resp.Message)
	}
	if resp.Terminal {
		t.Error("expected terminal=false for invalid input")
	}
}

func TestEngine_NotifyAction(t *testing.T) {
	e := newTestEngine(t)
	e.Handle(context.Background(), "+15005550001", "hi")
	e.Handle(context.Background(), "+15005550001", "2")
	resp, _ := e.Handle(context.Background(), "+15005550001", "Y")
	if resp.Action != "notify_responders" {
		t.Errorf("expected notify_responders action, got: %s", resp.Action)
	}
}
