package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mattventura/respond/internal/store"
)

func TestVoiceHandler_UnknownCaller_DialsAvailable(t *testing.T) {
	responders := newMockResponderStore(
		&store.Responder{PhoneNumber: "+15551111111", Available: true, IsValidated: true},
		&store.Responder{PhoneNumber: "+15552222222", Available: false, IsValidated: true},
	)
	sessions := newMockSessionStore()
	h := &VoiceHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}

	form := url.Values{"Caller-ID-Number": {"+15559999999"}, "Unique-ID": {"call-1"}}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "bridge") {
		t.Errorf("expected bridge action for unknown caller, got: %s", body)
	}
	if !strings.Contains(body, "15551111111") {
		t.Errorf("expected available responder in dial, got: %s", body)
	}
	if strings.Contains(body, "15552222222") {
		t.Errorf("unavailable responder should not be dialled, got: %s", body)
	}
}

func TestVoiceHandler_NoAvailableResponders(t *testing.T) {
	responders := newMockResponderStore(
		&store.Responder{PhoneNumber: "+15551111111", Available: false, IsValidated: true},
	)
	sessions := newMockSessionStore()
	h := &VoiceHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}

	form := url.Values{"Caller-ID-Number": {"+15559999999"}, "Unique-ID": {"call-1"}}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "no available responders") {
		t.Errorf("expected no-responders message, got: %s", body)
	}
}

func TestVoiceHandler_KnownResponder_UnvalidatedPromptsPINSetup(t *testing.T) {
	responders := newMockResponderStore(
		&store.Responder{PhoneNumber: "+15551111111", Available: false, IsValidated: false},
	)
	sessions := newMockSessionStore()
	h := &VoiceHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}

	form := url.Values{"Caller-ID-Number": {"+15551111111"}, "Unique-ID": {"call-2"}}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "PIN") {
		t.Errorf("expected PIN setup prompt, got: %s", body)
	}
	sess, err := sessions.Get(req.Context(), "call-2")
	if err != nil {
		t.Fatalf("session not created: %v", err)
	}
	if sess.State.Step != "responder_set_pin" {
		t.Errorf("expected step=responder_set_pin, got %s", sess.State.Step)
	}
}

func TestVoiceHandler_KnownResponder_ValidatedPromptsPIN(t *testing.T) {
	responders := newMockResponderStore(
		&store.Responder{PhoneNumber: "+15551111111", Available: true, IsValidated: true},
	)
	sessions := newMockSessionStore()
	h := &VoiceHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}

	form := url.Values{"Caller-ID-Number": {"+15551111111"}, "Unique-ID": {"call-3"}}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "PIN") {
		t.Errorf("expected PIN prompt, got: %s", body)
	}
	sess, err := sessions.Get(req.Context(), "call-3")
	if err != nil {
		t.Fatalf("session not created: %v", err)
	}
	if sess.State.Step != "responder_pin" {
		t.Errorf("expected step=responder_pin, got %s", sess.State.Step)
	}
}

func TestVoiceHandler_BadForm(t *testing.T) {
	h := &VoiceHandler{
		Responders: newMockResponderStore(),
		Sessions:   newMockSessionStore(),
		BaseURL:    "https://example.com",
	}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader("%invalid"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}
