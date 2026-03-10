package handler

import (
	"context"
	"fmt"

	"github.com/mattventura/respond/internal/store"
)

// mockResponderStore is an in-memory responderStore for handler tests.
type mockResponderStore struct {
	responders map[string]*store.Responder
}

func newMockResponderStore(responders ...*store.Responder) *mockResponderStore {
	m := &mockResponderStore{responders: map[string]*store.Responder{}}
	for _, r := range responders {
		m.responders[r.PhoneNumber] = r
	}
	return m
}

func (m *mockResponderStore) FindByPhone(_ context.Context, phone string) (*store.Responder, error) {
	r, ok := m.responders[phone]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return r, nil
}

func (m *mockResponderStore) ListAvailable(_ context.Context) ([]store.Responder, error) {
	var out []store.Responder
	for _, r := range m.responders {
		if r.Available {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *mockResponderStore) ListAll(_ context.Context) ([]store.Responder, error) {
	var out []store.Responder
	for _, r := range m.responders {
		out = append(out, *r)
	}
	return out, nil
}

func (m *mockResponderStore) SetPIN(_ context.Context, phone, pin string) error {
	r, ok := m.responders[phone]
	if !ok {
		return fmt.Errorf("not found")
	}
	r.PinHash = &pin
	return nil
}

func (m *mockResponderStore) SetValidated(_ context.Context, phone string) error {
	r, ok := m.responders[phone]
	if !ok {
		return fmt.Errorf("not found")
	}
	r.IsValidated = true
	return nil
}

func (m *mockResponderStore) ToggleAvailable(_ context.Context, phone string) (bool, error) {
	r, ok := m.responders[phone]
	if !ok {
		return false, fmt.Errorf("not found")
	}
	r.Available = !r.Available
	return r.Available, nil
}

func (m *mockResponderStore) UpdatePIN(_ context.Context, phone, pin string) error {
	r, ok := m.responders[phone]
	if !ok {
		return fmt.Errorf("not found")
	}
	r.PinHash = &pin
	return nil
}

func (m *mockResponderStore) Create(_ context.Context, phone string) error {
	if _, ok := m.responders[phone]; ok {
		return fmt.Errorf("already exists")
	}
	m.responders[phone] = &store.Responder{PhoneNumber: phone}
	return nil
}

func (m *mockResponderStore) Delete(_ context.Context, phone string) error {
	delete(m.responders, phone)
	return nil
}

func (m *mockResponderStore) CountByAvailability(_ context.Context) (int, int, error) {
	var active, inactive int
	for _, r := range m.responders {
		if r.Available {
			active++
		} else {
			inactive++
		}
	}
	return active, inactive, nil
}

func (m *mockResponderStore) SetAdmin(_ context.Context, phone string, isAdmin bool) error {
	r, ok := m.responders[phone]
	if !ok {
		return fmt.Errorf("not found")
	}
	r.IsAdmin = isAdmin
	return nil
}

// mockSessionStore is an in-memory sessionStore for handler tests.
type mockSessionStore struct {
	sessions map[string]*store.Session
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{sessions: map[string]*store.Session{}}
}

func (m *mockSessionStore) Get(_ context.Context, callSid string) (*store.Session, error) {
	s, ok := m.sessions[callSid]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *s
	stateCopy := s.State
	if stateCopy.Pending != nil {
		pendingCopy := make(map[string]string)
		for k, v := range stateCopy.Pending {
			pendingCopy[k] = v
		}
		stateCopy.Pending = pendingCopy
	}
	cp.State = stateCopy
	return &cp, nil
}

func (m *mockSessionStore) Upsert(_ context.Context, sess *store.Session) error {
	m.sessions[sess.CallSid] = sess
	return nil
}

func (m *mockSessionStore) Delete(_ context.Context, callSid string) error {
	delete(m.sessions, callSid)
	return nil
}
